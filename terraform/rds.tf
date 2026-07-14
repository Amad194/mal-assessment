# RDS Postgres (Multi-AZ) — the data tier. Local equivalent: the postgres
# container in docker-compose. Password is generated and stored ONLY in Secrets
# Manager; it never appears in code or plain state outputs.
resource "random_password" "db" {
  length  = 32
  special = false
}

resource "aws_security_group" "rds" {
  name_prefix = "accounts-rds-"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Postgres from EKS nodes only"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [module.eks.node_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

module "rds" {
  source  = "terraform-aws-modules/rds/aws"
  version = "~> 6.7"

  identifier = "accounts-db"

  engine               = "postgres"
  engine_version       = "16"
  family               = "postgres16"
  instance_class       = var.db_instance_class
  allocated_storage    = 20
  max_allocated_storage = 100
  storage_encrypted    = true # KMS at rest

  db_name  = var.db_name
  username = var.db_username
  password = random_password.db.result
  manage_master_user_password = false # we manage it via Secrets Manager below
  port     = 5432

  multi_az               = true # HA — automatic failover
  vpc_security_group_ids = [aws_security_group.rds.id]
  subnet_ids             = module.vpc.private_subnets
  create_db_subnet_group = true

  backup_retention_period = 14 # PITR window; banks retain longer in practice
  deletion_protection     = true
  skip_final_snapshot     = false

  performance_insights_enabled = true
  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]
}
