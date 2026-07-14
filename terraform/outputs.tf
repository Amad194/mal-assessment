output "cluster_name" {
  value = module.eks.cluster_name
}

output "cluster_endpoint" {
  value = module.eks.cluster_endpoint
}

output "ecr_repository_url" {
  value = aws_ecr_repository.accounts.repository_url
}

output "irsa_role_arn" {
  description = "Set as serviceAccount.roleArn in values-prod.yaml"
  value       = aws_iam_role.accounts_irsa.arn
}

output "consumer_irsa_role_arn" {
  description = "Set as serviceAccount.consumerRoleArn in values-prod.yaml"
  value       = aws_iam_role.accounts_consumer_irsa.arn
}

output "consumer_database_url_secret_arn" {
  value = aws_secretsmanager_secret.consumer_database_url.arn
}

output "msk_bootstrap_brokers_tls" {
  description = "Set as kafka.brokers in values-prod.yaml"
  value       = aws_msk_cluster.accounts.bootstrap_brokers_tls
}

output "rds_endpoint" {
  value = module.rds.db_instance_endpoint
}

output "database_url_secret_arn" {
  value = aws_secretsmanager_secret.database_url.arn
}
