# Separate IRSA role for the consumer. Least privilege and independent of the
# API role: it can read ONLY its own secret, consume the audit topic, and produce
# ONLY to the DLQ. A compromised consumer cannot read the API's secret nor write
# to the main audit topic.

data "aws_iam_policy_document" "consumer_irsa_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"
    principals {
      type        = "Federated"
      identifiers = [module.eks.oidc_provider_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:sub"
      values   = ["system:serviceaccount:${var.app_namespace}:${var.app_consumer_service_account}"]
    }
    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "accounts_consumer_irsa" {
  name               = "accounts-consumer-irsa"
  assume_role_policy = data.aws_iam_policy_document.consumer_irsa_assume.json
}

data "aws_iam_policy_document" "consumer_secrets_read" {
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    resources = [aws_secretsmanager_secret.consumer_database_url.arn]
  }
}

resource "aws_iam_role_policy" "consumer_secrets_read" {
  name   = "secrets-read"
  role   = aws_iam_role.accounts_consumer_irsa.id
  policy = data.aws_iam_policy_document.consumer_secrets_read.json
}

data "aws_iam_policy_document" "consumer_msk" {
  statement {
    effect    = "Allow"
    actions   = ["kafka-cluster:Connect", "kafka-cluster:DescribeCluster"]
    resources = [aws_msk_cluster.accounts.arn]
  }
  # Consume the audit topic + join the consumer group.
  statement {
    effect  = "Allow"
    actions = ["kafka-cluster:ReadData", "kafka-cluster:DescribeTopic"]
    resources = ["${replace(aws_msk_cluster.accounts.arn, ":cluster/", ":topic/")}/accounts.audit"]
  }
  statement {
    effect    = "Allow"
    actions   = ["kafka-cluster:AlterGroup", "kafka-cluster:DescribeGroup"]
    resources = ["${replace(aws_msk_cluster.accounts.arn, ":cluster/", ":group/")}/accounts-audit-consumer"]
  }
  # Produce ONLY to the DLQ topic.
  statement {
    effect  = "Allow"
    actions = ["kafka-cluster:WriteData", "kafka-cluster:DescribeTopic", "kafka-cluster:CreateTopic"]
    resources = ["${replace(aws_msk_cluster.accounts.arn, ":cluster/", ":topic/")}/accounts.audit.dlq"]
  }
}

resource "aws_iam_role_policy" "consumer_msk" {
  name   = "msk-consume-dlq"
  role   = aws_iam_role.accounts_consumer_irsa.id
  policy = data.aws_iam_policy_document.consumer_msk.json
}
