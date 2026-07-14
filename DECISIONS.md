# Decisions & Trade-offs

Part 2. One senior's reasoning, framed around the dominant constraint of a
regulated bank: **failure behaviour, auditability, and blast-radius control**.
Guiding principle from the brief — *one thing done correctly beats five things
half wired* — so this is a coherent vertical slice, productionised end to end.

---

## 1. The three decisions I'd defend hardest

**a) Draining is done in the pod lifecycle, not hoped for.**
Zero-downtime rollout is the first thing you read, so it's the thing I made
correct rather than plausible. On termination: the `preStop` hook calls
`/internal/drain` so **readiness fails first**, then sleeps `SHUTDOWN_DELAY` while
the pod keeps serving; only then does SIGTERM arrive and the app drains in-flight
connections — all inside `terminationGracePeriodSeconds: 40`, with
`maxUnavailable: 0` and a PDB floor of 2. *Trade-off:* every rollout is ~5s
slower per pod, and the drain logic lives in the app (distroless has no shell). I
take slower, correct rollouts over fast ones that drop requests.

**b) Messaging primitive: Kafka (→ MSK), consumed idempotently.**
A bank's audit/fraud/downstream fan-out wants a **replayable, ordered, durable
log**, not a queue that deletes on ack. Kafka gives partition-ordered, retained
events multiple consumer groups can read independently and **replay** after an
incident — exactly what an auditor needs. *Trade-off:* Kafka is heavier to run
than SQS/RabbitMQ and offset management is on me. I accept that for replayability
and ordering. Delivery is at-least-once; the consumer makes the **effect**
exactly-once via a `processed_events` dedupe ledger + a transactional upsert
(`store.go`), so a redelivered event is a no-op.

**c) Pooling: PgBouncer in transaction mode.**
Many small stateless replicas (3→20 under HPA) each holding a pgx pool would
exhaust RDS connection slots long before CPU. I'd front RDS with **PgBouncer in
transaction pooling mode** so a backend connection is held only for the duration
of a transaction — maximising reuse across a fleet of short-lived readers.
*Trade-off:* transaction mode forbids session-scoped state (session-level
`SET`, server-side prepared-statement caching, `LISTEN/NOTIFY`); the app must use
the simple protocol / disable statement caching. For a stateless SELECT service
that's free; I would *not* use transaction mode if the app needed session state.

---

## 2. SLO & alerting

- **SLI:** proportion of `GET /api/accounts/{id}` requests served **without a
  server error and within 300 ms**, measured at the server. Derived from user
  impact — a customer feels a slow or failed balance read, not CPU%.
- **Target / window:** **99.5%** over a **rolling 30 days** (≈3.6h error budget).
- **Paging that survives slow degradation:** a single static threshold (“page at
  1% errors”) misses a gradual bleed that sits at 0.9% for a day and quietly
  burns the whole budget. So I page on **error-budget burn rate over two
  windows**: fast burn = >14.4× budget over **both** 5m and 1h
  (`AccountsFastErrorBurn`). The long window catches the slow bleed; the short
  window makes it fast; requiring both suppresses false pages from a brief blip.
- **The one alert I'd keep:** `AccountsFastErrorBurn`. It's the closest proxy to
  "users are being harmed right now," and burn-rate framing means it fires for
  both a sharp outage and a slow degradation. Everything else
  (latency-p99, DB-down, dead-lettering) is diagnosis I can live without at 3am if
  I only get one page.

---

## 3. Least privilege & blast radius

If a pod is popped via RCE, here is what the attacker actually gets — and what
was deliberately denied.

**API pod (`accounts_api`):**
- *DB:* `SELECT` on `accounts` **only**. Cannot INSERT/UPDATE/DELETE, cannot run
  DDL, cannot read `audit_log`/`processed_events`, does not own any object — so it
  **cannot tamper with or erase the audit trail**, only read accounts it already
  serves.
- *Secrets:* its IRSA role reads **one** secret (`prod/accounts/database-url`).
  Not the consumer's secret, not "all secrets."
- *Network:* egress restricted by NetworkPolicy to DNS + 5432 + Kafka ports;
  IMDSv2-required blocks the classic SSRF→node-role credential theft.

**Consumer pod (`accounts_consumer`) — a separate SA, role, and secret:**
- *DB:* `INSERT/SELECT` on `audit_log` + `processed_events`; **no UPDATE/DELETE**
  (append-only, so it can't rewrite history) and **no access to `accounts`**.
- *Kafka:* `ReadData` on `accounts.audit`, `WriteData` on the **DLQ only** — it
  cannot forge events onto the main audit topic.

**Deliberately denied across the board:** superuser/owner DB rights, DDL at
runtime, cross-workload secret access, static long-lived AWS keys (OIDC/IRSA
only), and node-metadata credential access. The two workloads have **independent
blast radii**: compromising the read path yields no write access and no path to
the audit store.

---

## 4. Recovery

**Postgres.**
- **RPO ≈ 5 min**, backed by RDS automated backups + transaction-log archiving
  (PITR). A bank could push tighter with a synchronous replica, at a latency cost;
  5 min is the honest number for async PITR and is acceptable for this read
  service whose source-of-truth writes happen elsewhere.
- **RTO ≈ 30–60 min**: restore is a new RDS instance from a snapshot + PITR replay,
  then repoint the Secrets Manager endpoint. Multi-AZ handles *instance* failure
  automatically in ~1–2 min (that's HA, not backup); the RTO above is for the
  *data-loss/corruption* case a failover can't fix.
- **Proving the runbook before I need it:** a **scheduled restore drill** — a
  monthly job that restores the latest snapshot to a throwaway instance, runs a
  row-count + checksum against a known baseline, records the measured RTO, and
  alerts if the restore fails or exceeds target. An untested backup is a guess;
  the drill turns RPO/RTO from aspiration into a measured, alerting number.

**Messaging.**
- Offsets are committed **only after** the side effect succeeds, so events
  in-flight during an outage are **not lost** — on recovery they're redelivered
  and the dedupe ledger makes reprocessing a no-op. Unprocessed events sit safely
  in Kafka within the retention window; the consumer resumes from its last
  committed offset. Poison/persistently-failing messages are parked to the DLQ so
  one bad event can't block the partition, and are replayable once fixed.

---

## 5. What I cut, and what I'd do first in a real rollout

**Cut for the time box (approach noted so the reasoning scores):**
- **RDS IAM auth** — I shipped Secrets-Manager-sourced credentials; IAM auth
  (15-min tokens, no stored password) is strictly better and is change #1.
- **Real `terraform apply` + live plan** — validated only (no cloud account per
  the rules); `terraform/PLAN.txt` is the representative plan.
- **Service mesh mTLS**, **KEDA scaling on Kafka lag** (CPU HPA is adequate for
  this read service; lag-based scaling is the right call for the *consumer* and is
  next), **multi-env promotion** (structure is there via the values overlay +
  Argo project; I built one environment well), and **secret rotation Lambda**.
- **Per-workload NetworkPolicy CIDR-tightening** to the VPC range (currently
  egress is port-scoped but not CIDR-scoped).

**First three in a real rollout:** (1) RDS IAM auth, (2) the automated restore
drill wired to alerting, (3) KEDA lag-based autoscaling for the consumer. In that
order — credentials and proven recovery before scaling polish.
