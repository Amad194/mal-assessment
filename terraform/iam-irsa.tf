# IRSA: an IAM role the accounts-api pod (and ESO acting for it) assumes via the
# EKS OIDC provider. Least privilege — read one secret, connect to one MSK
# cluster/topic. No static credentials anywhere.

data "aws_iam_policy_document" "irsa_assume" {
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
      values   = ["system:serviceaccount:${var.app_namespace}:${var.app_service_account}"]
    }
    condition {
      test     = "StringEquals"
      variable = "${module.eks.oidc_provider}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "accounts_irsa" {
  name               = "accounts-api-irsa"
  assume_role_policy = data.aws_iam_policy_document.irsa_assume.json
}

# Read only the accounts database-url secret.
data "aws_iam_policy_document" "secrets_read" {
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    resources = [aws_secretsmanager_secret.database_url.arn]
  }
}

resource "aws_iam_role_policy" "secrets_read" {
  name   = "secrets-read"
  role   = aws_iam_role.accounts_irsa.id
  policy = data.aws_iam_policy_document.secrets_read.json
}

# Connect + write to the accounts MSK cluster/topic only.
data "aws_iam_policy_document" "msk_produce" {
  statement {
    effect    = "Allow"
    actions   = ["kafka-cluster:Connect", "kafka-cluster:DescribeCluster"]
    resources = [aws_msk_cluster.accounts.arn]
  }
  statement {
    effect  = "Allow"
    actions = ["kafka-cluster:WriteData", "kafka-cluster:DescribeTopic", "kafka-cluster:CreateTopic"]
    # Derive the topic ARN from the cluster ARN (…:cluster/… -> …:topic/…) and
    # scope to the audit topic only.
    resources = ["${replace(aws_msk_cluster.accounts.arn, ":cluster/", ":topic/")}/accounts.audit"]
  }
}

resource "aws_iam_role_policy" "msk_produce" {
  name   = "msk-produce"
  role   = aws_iam_role.accounts_irsa.id
  policy = data.aws_iam_policy_document.msk_produce.json
}
