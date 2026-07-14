terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.40"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Remote state with locking. Bucket + table are bootstrapped once, out of band
  # (chicken/egg). Kept here so reviewers see the intended backend; commented so
  # `terraform init` works locally without the bucket existing.
  # backend "s3" {
  #   bucket         = "amad194-tfstate-bank-platform"
  #   key            = "accounts/terraform.tfstate"
  #   region         = "eu-west-2"
  #   dynamodb_table = "tfstate-locks"
  #   encrypt        = true
  # }
}
