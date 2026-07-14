.DEFAULT_GOAL := help
IMAGE ?= ghcr.io/amad194/accounts-api:dev

.PHONY: help deps test build docker compose-up compose-down helm-lint helm-template tf-validate kind-up kind-load deploy-local

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n",$$1,$$2}'

deps: ## Resolve Go modules and write go.sum (both modules)
	cd app && go mod tidy
	cd consumer && go mod tidy

test: ## Run unit tests with race detector (both modules)
	cd app && go test -race ./...
	cd consumer && go test -race ./...

build: ## Build the Go binary
	cd app && CGO_ENABLED=0 go build -o ../bin/accounts-api .

docker: ## Build the container image
	docker build -t $(IMAGE) app

compose-up: ## Bring up Postgres + Kafka + app locally
	docker compose up --build -d

compose-down: ## Tear down the local stack
	docker compose down -v

helm-lint: ## Lint the chart
	cp db/migrations/*.up.sql deploy/helm/accounts/migrations/
	helm lint deploy/helm/accounts

helm-template: ## Render manifests to stdout
	helm template accounts deploy/helm/accounts

tf-validate: ## Validate Terraform (no backend/creds)
	cd terraform && terraform init -backend=false && terraform validate

kind-up: ## Create a local kind cluster
	kind create cluster --config scripts/kind-config.yaml --name bank-platform

kind-load: docker ## Load the local image into kind
	kind load docker-image $(IMAGE) --name bank-platform

deploy-local: ## Install the chart into kind (local secret path, no ESO/MSK)
	helm upgrade --install accounts deploy/helm/accounts \
	  --namespace accounts --create-namespace \
	  --set externalSecrets.enabled=false \
	  --set localSecret.databaseUrl='postgres://accounts:accounts@host.docker.internal:5432/accounts?sslmode=disable' \
	  --set image.repository=ghcr.io/amad194/accounts-api --set image.tag=dev \
	  --set serviceMonitor.enabled=false --set prometheusRule.enabled=false
