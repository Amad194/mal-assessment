# Decisions

Part 2 of the assessment: the reasoning that connects the build. This documents
what I optimised for, the trade-offs I made, and — just as important — what I
consciously left out in a 3–4 hour window.

> Guiding principle from the brief: *one thing done correctly beats five things
> half wired.* I built a **coherent vertical slice** — one service, correctly
> productionised end to end — rather than a broad but shallow platform.

---

## 1. What I optimised for, and priorities

A regulated bank's platform is judged less on features than on **failure
behaviour, auditability, and blast-radius control.** I ranked effort as:

1. **Correct reliability primitives** — liveness vs readiness that actually mean
   different things, graceful drain that doesn't drop requests, PDB + spread so
   maintenance can't take the service down.
2. **Security controls a bank is held to** — no static secrets, least-privilege
   identity, encryption in transit and at rest, a hardened runtime, a signed and
   scanned supply chain.
3. **Observability that supports an SLO** — RED metrics, burn-rate alerting, a
   dashboard, structured logs.
4. **Reproducible delivery** — IaC that validates, GitOps that's the single
   source of truth.

The application itself got the *least* effort by design — it's a trivial reader.

---

## 2. Architecture

```
                 Internet
                    │  TLS
             ┌──────▼───────┐
             │ Ingress (ALB │   (kind: ingress-nginx)
             │ /nginx)      │
             └──────┬───────┘
        NetworkPolicy: only ingress + Prometheus may reach the pods
             ┌──────▼────────────────────┐
             │  accounts-api (Deployment) │  3–20 replicas, HPA, PDB,
             │  distroless, non-root, RO  │  topology spread across AZs
             └───┬───────────────┬────────┘
     IRSA (no    │               │  audit events (best-effort, async)
     static keys)│               │
        ┌────────▼───┐     ┌─────▼──────┐
        │ RDS Postgres│     │ MSK (Kafka)│
        │ Multi-AZ,   │     │ 3 brokers, │
        │ KMS at rest │     │ TLS, IAM   │
        └─────────────┘     └────────────┘
   DATABASE_URL from Secrets Manager ──► External Secrets Operator ──► K8s Secret
```

Delivery: **GitHub Actions builds/scans/signs** → bumps the image tag in Git →
**Argo CD** reconciles the cluster to match Git.

---

## 3. The service

- **Go, stdlib HTTP** (`net/http` 1.22 method routing). Chosen for a tiny static
  binary → a `distroless/static` image with a near-zero CVE surface, fast cold
  start (matters for HPA scale-up under load), and because it's the idiom a bank
  SRE team expects. Dependencies kept to three: `pgx` (Postgres), `client_golang`
  (metrics), `kafka-go` (MSK).
- **Ports over globals.** `Store` and `Publisher` are interfaces so handlers are
  unit-tested against fakes with no live infra (`app/main_test.go`). That's what
  makes the reliability behaviour *testable*, not just asserted.
- **Liveness ≠ readiness.** `/healthz` only reports the process is alive and
  **must not** touch Postgres — otherwise a transient DB blip would make the
  kubelet kill and restart healthy pods, turning a dependency wobble into an
  outage. `/readyz` *does* check Postgres (with a 2s timeout) and also fails when
  draining, so traffic only lands on pods that can serve it.
- **Audit events off the critical path.** A read emits an `account.read` event to
  Kafka asynchronously and best-effort. A Kafka outage degrades the audit trail
  (alerted on) but never fails a customer read. The reverse trade-off — blocking
  reads on the audit write — would be wrong for availability.

---

## 4. Reliability on Kubernetes

| Control                       | Why                                                                    |
| ----------------------------- | ---------------------------------------------------------------------- |
| **Graceful drain in-process** | On SIGTERM: fail readiness → sleep `SHUTDOWN_DELAY` (endpoints converge) → drain connections within `SHUTDOWN_TIMEOUT`. Avoids the classic 502s during rollouts/scale-down. |
| `maxUnavailable: 0`           | Rollouts never dip below desired capacity.                             |
| **PodDisruptionBudget** (min 2) | Voluntary disruptions (node drains, upgrades) can't take the service below 2 replicas. |
| **topologySpreadConstraints** | Replicas spread across AZs → one AZ loss ≠ outage.                     |
| **HPA on CPU** (3→20)         | Absorbs traffic spikes; 5-min scale-down stabilisation avoids flapping.|
| **No preStop sleep**          | distroless has no shell — drain is handled in the app instead of a hook.|

**Why no CPU limit but a memory limit:** CPU limits cause CFS throttling that
adds tail latency to a latency-sensitive read path; requests + HPA already
protect neighbours. A memory limit stays, to turn a leak into a bounded OOMKill
rather than a noisy-neighbour node event.

---

## 5. Security controls (the bank part)

**Identity & secrets — no static credentials anywhere.**

- `DATABASE_URL` lives in **Secrets Manager**, generated by Terraform (the DB
  password never appears in code and isn't emitted as a plaintext output).
- **External Secrets Operator** projects it into a K8s Secret using the pod's
  **IRSA** role — the app never holds AWS keys.
- IRSA is **least privilege**: read *one* secret, connect/write *one* MSK topic.
  MSK uses **SASL/IAM**, so there are no Kafka passwords either.

**Runtime hardening (defence in depth).**

- `runAsNonRoot` (uid 65532), `readOnlyRootFilesystem`, `allowPrivilegeEscalation:
  false`, all capabilities dropped, `seccompProfile: RuntimeDefault`.
- distroless image: no shell, no package manager → nothing to pivot to.

**Network.** Default-deny `NetworkPolicy`; only the ingress controller and
Prometheus can reach the pods; egress limited to DNS, Postgres, and Kafka.

**Data protection.** KMS encryption at rest (EKS secrets, RDS, MSK, ECR);
`sslmode=require` to RDS; TLS in transit to MSK; IMDSv2 required on nodes (blocks
SSRF-based credential theft).

**Supply chain.** Immutable ECR tags; scan-on-push; **Trivy** gate on
HIGH/CRITICAL in CI; **SBOM + provenance**; **cosign** keyless signatures;
**gitleaks** secret scanning; **tfsec** on the IaC. Deploys are pinned to an
immutable image digest via the GitOps tag bump.

**Auditability.** EKS control-plane audit logs, RDS/MSK logs to CloudWatch, and
the application's own `account.read` audit stream — the trail a regulator asks for.

---

## 6. Data tier

- **RDS Postgres, Multi-AZ** for automatic failover; 14-day PITR; deletion
  protection; Performance Insights. Local equivalent is the Postgres container.
- **Migrations as a Helm pre-upgrade hook Job** (`golang-migrate`), so schema
  changes run *before* the new app version rolls out and are version-tracked and
  idempotent — not baked into app startup (which races across replicas).
- **Conservative pgx pool** (max 10/replica). Across 20 replicas that's a bounded
  200 connections — protecting RDS connection slots matters more than raw
  per-pod throughput for this workload.

---

## 7. Messaging tier

- **MSK / Kafka** as the event backbone — the credible choice for a bank's
  fraud/audit/downstream fan-out. Partitioned by account id (`kafka.Hash`) for
  per-account ordering; `RequireAll` acks for durability.
- Delivery is **at-least-once**; consumers must be idempotent. Producing is async
  and best-effort as described in §3.

---

## 8. CI/CD & GitOps

- **CI = build/verify/sign; CD = Argo CD reconcile.** CI never `kubectl apply`s
  to the cluster — it commits a tag to Git and Argo CD converges. This gives an
  auditable deploy history, trivial rollback (revert a commit), and drift
  correction (`selfHeal`). Argo's `AppProject` whitelists only the namespaced
  kinds this app needs — least privilege for the deployer too.
- Terraform and Helm each have their own validate pipeline so a bad manifest or
  plan fails on the PR, not in the cluster.

---

## 9. Observability

- **RED metrics**: request rate, errors (by status), duration histogram —
  labelled by a stable route name, not the raw `{id}` path, to avoid cardinality
  blow-ups.
- **An explicit SLO** (99.5% success on `get_account`) with **multi-window,
  multi-burn-rate alerting** — the fast-burn alert pages only when both the 5m
  and 1h windows exceed 14.4× budget, which suppresses false pages while still
  catching real regressions quickly. Plus latency-p99, DB-down, zero-ready, and
  audit-degraded alerts.
- Grafana dashboard checked in as code; structured JSON logs to stdout (→
  CloudWatch/Loki). See §12 for tracing.

---

## 10. Infrastructure as code

- Community modules for VPC/EKS/RDS (don't reinvent well-trodden infra), native
  resources for the bank-specific bits (ECR policy, MSK IAM auth, IRSA, secret
  composition).
- **Remote state on S3 + DynamoDB lock** (shown, commented so `init` works
  offline). Private subnets for all workloads and data; one NAT per AZ (no
  cross-AZ egress SPOF); 3 AZs throughout.

---

## 11. What I consciously left OUT (and why)

These are deliberate scope cuts, not oversights:

- **Write endpoints / real business logic** — the brief says the app isn't the
  point; adding writes would spend time without exercising new platform surface.
- **Distributed tracing (OTel)** — I'd wire OpenTelemetry → Tempo/X-Ray next; it's
  the highest-value *addition*, but metrics + logs cover the SLO story first.
- **Service mesh (mTLS between pods)** — a NetworkPolicy is the right first
  control; a mesh (Istio/Linkerd) is justified once there are many services, not
  one.
- **Multi-environment promotion (dev/stage/prod)** — structure is there (values
  overlay, Argo project) but I built one environment well rather than three
  thinly.
- **Secret rotation Lambda, WAF, PrivateLink, backup/restore drills, DR runbook**
  — named here so the reviewer sees I know they belong in a real bank; out of
  scope for the time box.
- **Autoscaling on custom metrics (RPS/latency via KEDA)** — CPU HPA is adequate
  for this workload; KEDA is the next step if CPU proves a poor proxy.

---

## 12. If I had another day

1. OpenTelemetry tracing end-to-end (ingress → app → Postgres/Kafka).
2. `terraform plan` in CI via a read-only OIDC role, posted to the PR.
3. A `dev` overlay + Argo ApplicationSet for env promotion.
4. Contract/integration tests against ephemeral Postgres + Kafka in CI
   (testcontainers).
5. Secrets Manager rotation + a documented DB failover / restore drill.

---

## Assumptions

- Cluster has kube-prometheus-stack, External Secrets Operator, and an ingress
  controller installed (platform add-ons, out of scope to provision here).
- GHCR stands in for ECR during the assessment; the Terraform provisions the real
  ECR repo for the production path.
- Single region, single environment for the time box; the design extends to
  multi-region/multi-env without rework.
