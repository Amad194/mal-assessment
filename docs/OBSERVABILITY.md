# Observability

## RED metrics

Exposed at `GET /metrics` (Prometheus). Scraped via the `ServiceMonitor`
(kube-prometheus-stack).

**API** (`app/metrics.go`):
- **Rate** — `http_requests_total{route,method,status}`
- **Errors** — the `status=~"5.."` slice of the above
- **Duration** — `http_request_duration_seconds_bucket{route,method}` (histogram)
- Plus `accounts_db_up` and `accounts_audit_publish_failures_total`.

**Consumer** (`consumer/metrics.go`):
- `consumer_messages_processed_total`, `consumer_messages_deduplicated_total`,
  `consumer_messages_dead_lettered_total`, `consumer_processing_duration_seconds`.

Route labels use a **stable name** (`get_account`), never the raw `{id}` path, to
avoid cardinality blow-ups.

## Trace path

OpenTelemetry is wired in `app/otel.go`. The mux is wrapped with
`otelhttp.NewHandler`, so every request gets a server span; `handleGetAccount`
opens a child span `db.get_account` around the Postgres read. That gives a trace
path **HTTP request → DB query** through the service.

Wiring (no code change to switch backends):
- Enable by setting `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g.
  `http://otel-collector.monitoring.svc:4317`) — see `values.yaml` `otel.endpoint`.
- Exporter: OTLP/gRPC → an **OpenTelemetry Collector**, which fans out to Tempo /
  AWS X-Ray / Honeycomb. Sampling: parent-based, 10% head sampling.
- Unset endpoint ⇒ a no-op tracer, so local runs and tests need no collector.

## Dashboard (panel-as-code)

`observability/dashboards/accounts.json` — request rate by status, error ratio
vs the 0.5% SLO, latency p50/p90/p99, DB-up, and audit-publish failures.

## Alerting

`deploy/helm/accounts/templates/prometheusrule.yaml`. The one that **pages**:

```
AccountsFastErrorBurn: error-budget burn > 14.4x over BOTH 5m and 1h windows
```

A multi-window burn-rate alert (see DECISIONS.md §SLO) — it catches a fast
outage in minutes and a slow, gradual degradation that never trips a single
static threshold, while suppressing false pages. Others: `AccountsHighLatencyP99`,
`AccountsDBDown`, `AccountsNoReadyReplicas`, `ConsumerDeadLettering`.

## PII scrubbing — and what it still misses

The `accounts` row contains a customer **name** (PII) and **balance** (sensitive).

**What we scrub before anything leaves the process:**
- **Logs** are structured (`slog`) and log *identifiers only* — `account_id`
  (a UUID, not a natural-person identifier), never `name` or `balance`. Error
  logs carry the id and error, not the row.
- **Metrics** carry no PII and no high-cardinality per-account labels.
- **Traces** set `account.id` only; the row body is never attached as a span
  attribute. Sampling is head-based so bodies are never needed.
- **Audit events** on Kafka carry `account_id` + `event_type` + timestamps — no
  name/balance.

**What field-level scrubbing still misses (honest limits):**
- `account_id` is a **pseudonym, not anonymisation** — joinable back to the person
  via the DB, so exported telemetry is still personal data under GDPR and must be
  access-controlled and retention-bounded.
- **Free-text error strings** can leak: a driver error echoing a query parameter,
  or a panic stack with a value in scope, bypasses field-level rules. Mitigate
  with an egress scrubber/redaction processor in the Collector, not just
  discipline at call sites.
- **Timing + volume** are themselves a side channel (which account was read, how
  often) even with values removed.
- A **compromised process** can read the cleartext row regardless of what we
  scrub on export — scrubbing protects the telemetry pipeline, not the app.
