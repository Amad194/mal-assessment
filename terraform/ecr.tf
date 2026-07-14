# ECR is the production image registry (mapped from ghcr.io used by CI for the
# assessment). Scan-on-push + immutable tags + lifecycle expiry.
resource "aws_ecr_repository" "accounts" {
  name                 = "accounts-api"
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  encryption_configuration {
    encryption_type = "KMS"
  }
}

resource "aws_ecr_lifecycle_policy" "accounts" {
  repository = aws_ecr_repository.accounts.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep last 20 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 20
      }
      action = { type = "expire" }
    }]
  })
}
