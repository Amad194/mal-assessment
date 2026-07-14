# accounts-api — bank platform take-home

A containerized Go service on Kubernetes with its data tier (Postgres/RDS),
messaging tier (Kafka/MSK), CI/CD, observability, and the security controls a
regulated bank is held to. The application is deliberately trivial — **the
platform around it is the deliverable.**

- **Part 1 — the build:** this repository.
- **Part 2 — the reasoning:** [`DECISIONS.md`](./DECISIONS.md).

Everything is authored and validated with free, local tooling. Nothing is
applied to a real cloud. Every place where I would normally `terraform apply`
against AWS is called out explicitly and mapped to its AWS service.

---

## The service (contract)

| Endpoint                 | Purpose                                                        |
| ------------------------ | ------------------------------------------------------------- |
| `GET /healthz`           | Liveness — process is up. Never touches Postgres.             |
| `GET /readyz`            | Readiness — **503 while draining or while Postgres is down.** |
| `GET /metrics`           | Prometheus exposition (app + Go runtime metrics).             |
| `GET /api/accounts/{id}` | Reads one row from Postgres; emits a best-effort audit event. |

Seeded ids: `11111111-1111-1111-1111-111111111111`, `22222222-2222-2222-2222-222222222222`.

---

## Repository layout

```
app/                     API service (Go: HTTP + pgx + prometheus + kafka-go + OTel)
consumer/                Idempotent Kafka consumer (dedupe ledger + poison->DLQ)
db/migrations/           SQL migrations (golang-migrate format)
db/roles.sql             Least-privilege DB roles (accounts_api, accounts_consumer)
deploy/helm/accounts/    Helm chart: API + consumer Deployments, Services, probes,
                         HPA, PDB, NetworkPolicy, ExternalSecrets, ServiceMonitors,
                         PrometheusRule, migrate Job, per-workload ServiceAccounts
deploy/argocd/           Argo CD AppProject + Application (GitOps CD)
terraform/               VPC, EKS, ECR, RDS, MSK, IRSA (x2), Secrets Manager, PLAN.txt
observability/           Grafana dashboard (alerts live in the chart's PrometheusRule)
docs/                    CREDENTIALS.md, OBSERVABILITY.md (+PII), ROLLBACK.md, OIDC policy
.github/workflows/       ci (api), ci-consumer, terraform, helm, security
scripts/                 kind cluster config
docker-compose.yaml      Local stack: Postgres + Kafka + API + consumer
```

## AWS ⇄ local mapping

| AWS service         | Used for                     | Local equivalent here                    |
| ------------------- | ---------------------------- | ---------------------------------------- |
| EKS                 | Kubernetes control plane     | kind / minikube (`scripts/kind-config`)  |
| ECR                 | Image registry               | ghcr.io (CI) / local build               |
| RDS Postgres        | Data tier                    | `postgres` container (docker-compose)    |
| MSK (Kafka)         | Messaging tier               | `kafka` container (docker-compose)       |
| Secrets Manager     | `DATABASE_URL` at rest       | Helm `localSecret` (kind) / plain Secret |
| IAM (IRSA)          | Pod → AWS authz, no static keys | n/a locally (noop publisher)          |
| ELB/ALB             | Ingress                      | kind ingress-nginx                       |
| MSK DLQ topic       | `accounts.audit.dlq`         | same topic on local Kafka                |
| OTel Collector      | trace export (OTLP)          | set `OTEL_EXPORTER_OTLP_ENDPOINT` or off |

### Deep-dive docs
- [`docs/CREDENTIALS.md`](docs/CREDENTIALS.md) — secret flow + AWS mapping (no plaintext)
- [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md) — RED metrics, trace path, alert, **PII scrubbing**
- [`docs/ROLLBACK.md`](docs/ROLLBACK.md) — deploy (OIDC + approval) & rollback path
- [`db/roles.sql`](db/roles.sql) — least-privilege roles; [`terraform/PLAN.txt`](terraform/PLAN.txt) — sanitised plan
- [`DECISIONS.md`](DECISIONS.md) — Part 2

---

## Bring it up

### A. Fastest — full stack with docker-compose (recommended)

```bash
docker compose up --build -d
curl localhost:8080/healthz
curl localhost:8080/readyz
curl localhost:8080/api/accounts/11111111-1111-1111-1111-111111111111
curl localhost:8080/metrics | head
```

`/readyz` returns 503 until Postgres is healthy, then 200. Reading an account
publishes an `account.read` event to `accounts.audit`; the **consumer** records it
idempotently in `audit_log`. Verify the consumer + idempotency:

```bash
# read the same account 3x -> 3 events published, exactly 1 audit row
for i in 1 2 3; do curl -s localhost:8080/api/accounts/11111111-1111-1111-1111-111111111111 >/dev/null; done
docker compose exec postgres psql -U accounts -c "SELECT count(*) FROM audit_log;"        # 1
docker compose exec postgres psql -U accounts -c "SELECT count(*) FROM processed_events;" # 1
curl -s localhost:8081/metrics | grep consumer_messages   # processed / deduplicated / dead_lettered
```

### B. On Kubernetes (kind)

```bash
make kind-up           # 3-node kind cluster
docker compose up -d postgres   # reuse the local Postgres
make kind-load         # build + load the image
make deploy-local      # helm install with the local-secret path (no ESO/MSK)
kubectl -n accounts port-forward svc/accounts-accounts 8080:80
```

### C. Validate without running anything

```bash
make test           # go test -race
make helm-template  # render all manifests
make helm-lint
make tf-validate    # terraform init -backend=false && terraform validate
```

> **Go modules:** `go.sum` is generated by `make deps` (`go mod tidy`) — it can't
> be authored by hand (hashes are content-addressed). CI runs `go mod download`
> before build. Run `make deps` once after cloning if building locally.

---

## What CI does (GitHub Actions, free runners)

- **`ci.yaml`** — `go vet` + staticcheck + `go test -race`; build & push image to
  GHCR; **Trivy** image scan (fails on HIGH/CRITICAL); SBOM + provenance;
  **cosign** keyless signing; then bumps the image tag in `values.yaml` and
  commits it — Argo CD syncs the change (GitOps).
- **`terraform.yaml`** — `fmt -check`, `validate`, **tfsec**.
- **`helm.yaml`** — `helm lint`, `helm template`, **kubeconform** schema validation.
- **`security.yaml`** — gitleaks secret scan + Trivy fs (vuln/misconfig/secret),
  SARIF to GitHub code scanning; weekly re-scan for new CVEs.

---

## Production deploy path (would-apply-to-AWS)

```bash
cd terraform && terraform init && terraform apply      # <-- not run here
# feed outputs into the prod overlay:
#   irsa_role_arn            -> serviceAccount.roleArn
#   msk_bootstrap_brokers_tls-> kafka.brokers
# then commit; Argo CD deploys deploy/helm/accounts with values-prod.yaml.
```

See [`DECISIONS.md`](./DECISIONS.md) for the reasoning, trade-offs, and the
explicit list of what I left out and why.
