#!/usr/bin/env bash
# Generates one self-contained HTML file per assessment upload slot, under
# submission/. Open each in a browser and Print -> Save as PDF (the upload slots
# accept PDF). No external tools required.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=submission
mkdir -p "$OUT"

# Start an HTML doc.
start() { # $1=outfile $2=title
  cat > "$1" <<HTML
<!doctype html><html><head><meta charset="utf-8"><title>$2</title>
<style>
 body{font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;max-width:1000px;margin:24px auto;padding:0 20px;color:#111}
 h1{border-bottom:3px solid #111;padding-bottom:6px}
 h2{margin-top:28px;background:#111;color:#fff;padding:6px 10px;font-size:14px;font-family:ui-monospace,Consolas,monospace}
 pre{background:#f6f8fa;border:1px solid #d0d7de;border-radius:6px;padding:12px;overflow:auto;font:12px/1.45 ui-monospace,Consolas,monospace;white-space:pre-wrap;word-break:break-word}
 .note{background:#fff8c5;border:1px solid #d4a72c;border-radius:6px;padding:10px 12px;font-size:13px}
</style></head><body><h1>$2</h1>
HTML
}

# Append a file as an escaped code block.
add() { # $1=outfile $2=path
  { printf '<h2>%s</h2>\n<pre>' "$2"
    sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' "$2"
    printf '</pre>\n'; } >> "$1"
}

note() { printf '<div class="note">%s</div>\n' "$2" >> "$1"; }
end()  { printf '</body></html>\n' >> "$1"; }

# 1. Dockerfile
f="$OUT/01-dockerfile.html"; start "$f" "Deliverable 1 — Dockerfile (multi-stage, non-root, digest-pinned, HEALTHCHECK)"
note "$f" "Multi-stage; final image is distroless/static pinned to a <b>digest</b>; runs as numeric non-root 65532; a HEALTHCHECK via the binary's -health mode (distroless has no curl). read-only root FS + dropped Linux caps are enforced at runtime by the Helm securityContext (deliverable 2)."
add "$f" app/Dockerfile; add "$f" consumer/Dockerfile; end "$f"

# 2. Kubernetes / Helm
f="$OUT/02-kubernetes.html"; start "$f" "Deliverable 2 — Kubernetes / Helm (zero-downtime, PDB, securityContext, HPA)"
note "$f" "Zero-downtime rollout: readiness probe + preStop hook (-presleep flips /readyz to 503 then sleeps) + terminationGracePeriodSeconds, with maxUnavailable:0. Plus PDB, numeric securityContext, HPA, topology spread, NetworkPolicy."
for x in deployment service hpa pdb networkpolicy serviceaccount configmap externalsecret migrate-job consumer-deployment; do add "$f" "deploy/helm/accounts/templates/$x.yaml"; done
add "$f" deploy/helm/accounts/values.yaml; end "$f"

# 3. Postgres + least-privilege roles
f="$OUT/03-postgres-and-roles.html"; start "$f" "Deliverable 3 — Postgres provisioning + least-privilege DB roles"
note "$f" "RDS via Terraform (Multi-AZ, KMS, 14d PITR). Two runtime roles: accounts_api (SELECT only) and accounts_consumer (INSERT/SELECT on audit tables, no UPDATE/DELETE, no access to accounts). Each role's deliberate 'cannot do' is commented inline."
add "$f" db/roles.sql; add "$f" db/migrations/0001_init.up.sql; add "$f" db/migrations/0002_audit.up.sql; add "$f" terraform/rds.tf; end "$f"

# 4. Credential handling
f="$OUT/04-credentials.html"; start "$f" "Deliverable 4 — Credential handling (no plaintext; Secrets Manager + IRSA)"
add "$f" docs/CREDENTIALS.md; add "$f" deploy/helm/accounts/templates/externalsecret.yaml; add "$f" terraform/secrets.tf; add "$f" terraform/iam-irsa.tf; end "$f"

# 5. Idempotent consumer
f="$OUT/05-consumer.html"; start "$f" "Deliverable 5 — Idempotent consumer (dedupe + poison -> DLQ)"
note "$f" "At-least-once delivery -> exactly-once effect via processed_events dedupe ledger + transactional upsert (store.go). Poison messages park to the DLQ; transient faults retry with backoff then park. Tests prove all paths (processor_test.go)."
for x in main processor store dlq event metrics processor_test; do add "$f" "consumer/$x.go"; done; end "$f"

# 6. CI/CD
f="$OUT/06-cicd.html"; start "$f" "Deliverable 6 — CI/CD (scan gate, immutable tags, OIDC, approval, rollback)"
note "$f" "Trivy scan gates the merge; images pushed with immutable SHA tags + cosign signature; deploy uses GitHub OIDC (no static keys) gated on the 'production' environment (manual approval); rollback documented."
add "$f" .github/workflows/ci.yaml; add "$f" .github/workflows/ci-consumer.yaml; add "$f" docs/oidc-trust-policy.json; add "$f" docs/ROLLBACK.md; end "$f"

# 7. Terraform
f="$OUT/07-terraform.html"; start "$f" "Deliverable 7 — Terraform (EKS/IAM/ECR/RDS/MSK/secrets) + sanitised plan"
add "$f" terraform/PLAN.txt
for x in versions providers variables vpc eks ecr rds msk secrets iam-irsa iam-irsa-consumer outputs; do add "$f" "terraform/$x.tf"; done; end "$f"

# 8. Observability
f="$OUT/08-observability.html"; start "$f" "Deliverable 8 — Observability (RED, trace, dashboard, alert, PII)"
add "$f" docs/OBSERVABILITY.md; add "$f" app/metrics.go; add "$f" app/otel.go; add "$f" deploy/helm/accounts/templates/prometheusrule.yaml; add "$f" observability/dashboards/accounts.json; end "$f"

# 9. README, 10. Decisions
f="$OUT/09-readme.html"; start "$f" "README.md"; add "$f" README.md; end "$f"
f="$OUT/10-decisions.html"; start "$f" "Part 2 — Decisions & Trade-offs"; add "$f" DECISIONS.md; end "$f"

echo "Generated:"; ls -1 "$OUT"
