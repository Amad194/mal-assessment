# The app consumes a single DATABASE_URL secret. We compose it from the RDS
# endpoint + generated password and store it in Secrets Manager. External
# Secrets Operator (in-cluster) reads this via the IRSA role and projects it
# into a Kubernetes Secret. sslmode=require enforces TLS to RDS.
resource "aws_secretsmanager_secret" "database_url" {
  name        = "prod/accounts/database-url"
  description = "accounts-api Postgres connection string"
  # KMS-encrypted by default; rotation can be attached with a Lambda later.
}

resource "aws_secretsmanager_secret_version" "database_url" {
  secret_id = aws_secretsmanager_secret.database_url.id
  secret_string = format(
    "postgres://%s:%s@%s/%s?sslmode=require",
    var.db_username,
    random_password.db.result,
    module.rds.db_instance_endpoint,
    var.db_name,
  )
}
