# Deploy & Rollback

## Deploy (GitOps)

CI never `kubectl apply`s. On merge to `main`:
1. `ci.yaml` builds → **Trivy scan gate** → pushes `ghcr.io/amad194/accounts-api:<sha>`
   (immutable tag) → SBOM + cosign signature → commits the new tag into
   `values.yaml`.
2. **Argo CD** sees the commit and reconciles the cluster to match.
3. The `deploy-production` job (gated on the `production` GitHub Environment with
   **required reviewers** = manual approval) assumes an AWS role via **OIDC**
   (`docs/oidc-trust-policy.json`, no static keys) for any push-based/`kubectl`
   step. It is skipped until `vars.AWS_DEPLOY_ROLE_ARN` is set.

## Rollback path

Because production images are **immutable, versioned tags**, every prior release
is still pullable — rollback is deterministic.

- **GitOps (primary):** `git revert <bump-commit>` on `main`. Argo CD re-syncs the
  previous tag automatically. Fully audited (the rollback is itself a commit).
- **Argo CD:** `argocd app rollback accounts <history-id>` — instant revert to a
  known-good sync, no Git round-trip.
- **Helm (push-based):** `helm rollback accounts <REVISION>` (chart keeps history).
- **Fast mitigation:** `kubectl -n accounts rollout undo deploy/accounts-accounts`.

RollingUpdate with `maxUnavailable: 0` + the readiness/preStop drain means the
rollback itself is zero-downtime. The migrate Job uses idempotent, additive
migrations, so an app rollback does not require a schema rollback.
