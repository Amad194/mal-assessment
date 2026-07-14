# MSK — the messaging tier (local equivalent: the kafka container in
# docker-compose). TLS in transit, KMS at rest, private subnets only.
resource "aws_security_group" "msk" {
  name_prefix = "accounts-msk-"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Kafka TLS from EKS nodes"
    from_port       = 9094
    to_port         = 9094
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

resource "aws_msk_cluster" "accounts" {
  cluster_name           = "accounts-events"
  kafka_version          = "3.6.0"
  number_of_broker_nodes = 3 # one per AZ

  broker_node_group_info {
    instance_type   = "kafka.m5.large"
    client_subnets  = module.vpc.private_subnets
    security_groups = [aws_security_group.msk.id]
    storage_info {
      ebs_storage_info { volume_size = 100 }
    }
  }

  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
      in_cluster    = true
    }
  }

  # IAM-based auth so producers use the pod's IRSA role — no static Kafka creds.
  client_authentication {
    sasl { iam = true }
  }

  logging_info {
    broker_logs {
      cloudwatch_logs {
        enabled   = true
        log_group = aws_cloudwatch_log_group.msk.name
      }
    }
  }
}

resource "aws_cloudwatch_log_group" "msk" {
  name              = "/aws/msk/accounts-events"
  retention_in_days = 30
}
