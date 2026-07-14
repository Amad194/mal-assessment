module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.8"

  cluster_name    = var.cluster_name
  cluster_version = var.kubernetes_version

  # Private API endpoint; public access restricted to admin CIDRs in a real
  # deployment. Kept public+private here so a bastion-less reviewer could reach it.
  cluster_endpoint_public_access  = true
  cluster_endpoint_private_access = true

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  # Control-plane audit + authenticator logs to CloudWatch (regulatory).
  cluster_enabled_log_types = ["api", "audit", "authenticator", "controllerManager", "scheduler"]

  # Secrets encryption at rest with a dedicated KMS key (envelope encryption).
  cluster_encryption_config = {
    resources = ["secrets"]
  }

  enable_irsa = true

  eks_managed_node_groups = {
    default = {
      instance_types = ["m6i.large"]
      min_size       = 3
      max_size       = 6
      desired_size   = 3
      # Nodes in private subnets, IMDSv2 required (blocks SSRF creds theft).
      metadata_options = {
        http_tokens                 = "required"
        http_put_response_hop_limit = 1
      }
    }
  }

  # Managed add-ons kept current by EKS.
  cluster_addons = {
    coredns                = {}
    kube-proxy             = {}
    vpc-cni                = {}
    aws-ebs-csi-driver     = {}
  }
}
