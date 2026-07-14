provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "bank-platform"
      Service     = "accounts-api"
      Environment = var.environment
      ManagedBy   = "terraform"
      Owner       = "platform"
    }
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}
