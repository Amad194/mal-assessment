# Credential Handling

**Rule: no plaintext credential in the repo or in any committed manifest.** Grep
the tree — the only secret-shaped strings are local-dev throwaways in
`docker-compose.yaml` (`accounts:accounts`), never used in a cluster.

## How the credential reaches the pod (production)

```
AWS Secrets Manager                     EKS cluster
┌───────────────────────────┐          ┌──────────────────────────────────────┐
│ prod/accounts/             │          │ External Secrets Operator             │
│   database-url  (API)      │◀────────▶│  (ClusterSecretStore: aws-secrets-mgr)│
│   consumer-database-url    │  reads   │        │ assumes IRSA role via OIDC    │
└───────────────────────────┘          │        ▼                              │
                                        │  Kubernetes Secret  accounts-*-db     │
                                        │        │ mounted as env DATABASE_URL   │
                                        │        ▼                              │
                                        │  accounts-api / accounts-consumer pod │
                                        └──────────────────────────────────────┘
```

1. Terraform generates the DB password (`random_password`) and writes the full
   connection string to **Secrets Manager** (`terraform/secrets.tf`). The
   password is never an output and never touches Git.
2. **External Secrets Operator** (`templates/externalsecret.yaml`) syncs that
   value into a native Kubernetes `Secret` on a 1h refresh. ESO authenticates to
   AWS with the workload's **IRSA** role — no static AWS keys in the cluster.
3. The Deployment projects the Secret into the container as `DATABASE_URL`
   (`secretKeyRef`). The app reads it from the environment at startup.
4. Rotation: rotate in Secrets Manager → ESO re-syncs → pods pick it up on
   restart (or via a reloader). No image rebuild, no manifest change.

## Least privilege, two ways

- **Two separate secrets, two IRSA roles.** The API reads only
  `prod/accounts/database-url`; the consumer reads only
  `prod/accounts/consumer-database-url` (`terraform/iam-irsa*.tf`). Neither can
  read the other's.
- **Two DB roles** (`db/roles.sql`): `accounts_api` (SELECT on `accounts`) and
  `accounts_consumer` (INSERT/SELECT on `audit_log` + `processed_events`). The
  credential a pod holds is only as powerful as its DB role.

## AWS mechanism mapping

| Concern | This repo | Production AWS |
|---|---|---|
| Secret at rest | ESO `ClusterSecretStore` | **Secrets Manager** (KMS-encrypted) |
| Pod → AWS auth | IRSA annotation on the SA | **IRSA** (OIDC) — or EKS **Pod Identity** |
| No static keys | ESO uses the SA's role | IAM role assumed via OIDC web identity |
| Alternative | — | **RDS IAM auth** (short-lived token, no stored password) |

**Why not RDS IAM auth here?** It's the strongest option (no stored password at
all) and is the natural next step. I used Secrets-Manager-sourced credentials
because it works identically for the non-RDS local stack and keeps the app's
connection code unchanged; the trade-off is a stored (rotatable) password versus
IAM's 15-minute tokens.
