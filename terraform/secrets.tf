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

# The consumer connects as the least-privilege `accounts_consumer` role (see
# db/roles.sql), with its own credential so its blast radius is separate from
# the API's. The role must be created with this same password:
#   psql ... -f db/roles.sql -v consumer_password='<this value>'
resource "random_password" "consumer_db" {
  length  = 32
  special = false
}

resource "aws_secretsmanager_secret" "consumer_database_url" {
  name        = "prod/accounts/consumer-database-url"
  description = "accounts-consumer Postgres connection string (accounts_consumer role)"
}

resource "aws_secretsmanager_secret_version" "consumer_database_url" {
  secret_id = aws_secretsmanager_secret.consumer_database_url.id
  secret_string = format(
    "postgres://accounts_consumer:%s@%s/%s?sslmode=require",
    random_password.consumer_db.result,
    module.rds.db_instance_endpoint,
    var.db_name,
  )
}
