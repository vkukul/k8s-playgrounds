# Variables
BINARY_NAME := secret-operator
BINARY_PATH := bin/$(BINARY_NAME)
IMAGE_NAME := secret-operator
IMAGE_TAG := latest
GO := go
GOFLAGS := -v
MAIN_PATH := ./cmd/secret-operator

GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

# =============================================================================
# Build Commands
# =============================================================================

.PHONY: build
build: ## Build the operator binary
	@echo "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	$(GO) build $(GOFLAGS) -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "$(GREEN)Binary built at $(BINARY_PATH)$(NC)"

.PHONY: run
run: ## Run the operator locally (uses current kubeconfig)
	@echo "$(GREEN)Running $(BINARY_NAME)...$(NC)"
	$(GO) run $(MAIN_PATH)/main.go

.PHONY: clean
clean: ## Remove build artifacts
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	rm -rf bin/
	$(GO) clean

# =============================================================================
# Testing & Quality
# =============================================================================

.PHONY: test
test: ## Run unit tests
	@echo "$(GREEN)Running tests...$(NC)"
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

.PHONY: lint
lint: ## Run linter (requires golangci-lint)
	@echo "$(GREEN)Running linter...$(NC)"
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format Go code
	@echo "$(GREEN)Formatting code...$(NC)"
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	@echo "$(GREEN)Running go vet...$(NC)"
	$(GO) vet ./...

# =============================================================================
# Docker
# =============================================================================

.PHONY: docker-build
docker-build: ## Build the Docker image
	@echo "$(GREEN)Building Docker image $(IMAGE_NAME):$(IMAGE_TAG)...$(NC)"
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

.PHONY: docker-load
docker-load: ## Load the Docker image into minikube
	@echo "$(GREEN)Loading image into minikube...$(NC)"
	minikube image load $(IMAGE_NAME):$(IMAGE_TAG)
	@echo "$(GREEN)Image loaded. Verify with: minikube image ls | grep $(IMAGE_NAME)$(NC)"

# =============================================================================
# Deploy to Kubernetes
# =============================================================================

.PHONY: deploy
deploy: ## Deploy the operator to the minikube cluster
	@echo "$(GREEN)Deploying $(BINARY_NAME) to cluster...$(NC)"
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/serviceaccount.yaml
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/deployment.yaml
	@echo "$(GREEN)Deployed! Check status with: kubectl get pods -n secret-operator$(NC)"

.PHONY: undeploy
undeploy: ## Remove the operator from the cluster
	@echo "$(YELLOW)Removing $(BINARY_NAME) from cluster...$(NC)"
	kubectl delete -f deploy/deployment.yaml --ignore-not-found
	kubectl delete -f deploy/rbac.yaml --ignore-not-found
	kubectl delete -f deploy/serviceaccount.yaml --ignore-not-found
	kubectl delete -f deploy/namespace.yaml --ignore-not-found

.PHONY: deploy-logs
deploy-logs: ## Tail logs from the deployed operator pod
	kubectl logs -f -n secret-operator -l app=secret-operator

.PHONY: deploy-status
deploy-status: ## Show the status of the deployed operator
	@echo "$(GREEN)Deployment status:$(NC)"
	kubectl get pods -n secret-operator
	@echo ""
	kubectl get deployment -n secret-operator

.PHONY: deploy-restart
deploy-restart: ## Restart the deployed operator (picks up new image)
	kubectl rollout restart deployment/secret-operator -n secret-operator

# =============================================================================
# Minikube Cluster Commands
# =============================================================================

.PHONY: cluster-start
cluster-start: ## Start minikube cluster
	@echo "$(GREEN)Starting minikube cluster...$(NC)"
	minikube start --driver=docker
	@echo "$(GREEN)Cluster started! Verifying...$(NC)"
	kubectl cluster-info

.PHONY: cluster-stop
cluster-stop: ## Stop minikube cluster
	@echo "$(YELLOW)Stopping minikube cluster...$(NC)"
	minikube stop

.PHONY: cluster-delete
cluster-delete: ## Delete minikube cluster entirely
	@echo "$(YELLOW)Deleting minikube cluster...$(NC)"
	minikube delete

.PHONY: cluster-status
cluster-status: ## Show minikube cluster status
	@echo "$(GREEN)Cluster Status:$(NC)"
	minikube status
	@echo ""
	@echo "$(GREEN)Nodes:$(NC)"
	kubectl get nodes

.PHONY: cluster-dashboard
cluster-dashboard: ## Open minikube dashboard in browser
	@echo "$(GREEN)Opening Kubernetes dashboard...$(NC)"
	minikube dashboard

# =============================================================================
# Test Resources
# =============================================================================

.PHONY: create-test-secret
create-test-secret: ## Create a test secret with expiration annotation
	@echo "$(GREEN)Creating test secret...$(NC)"
	kubectl apply -f scripts/test-secrets.yaml
	@echo "$(GREEN)Test secrets created. View with: kubectl get secrets$(NC)"

.PHONY: delete-test-secret
delete-test-secret: ## Delete test secrets
	@echo "$(YELLOW)Deleting test secrets...$(NC)"
	kubectl delete -f scripts/test-secrets.yaml --ignore-not-found

.PHONY: show-secrets
show-secrets: ## Show all secrets with our annotations
	@echo "$(GREEN)Secrets with expiration annotations:$(NC)"
	kubectl get secrets -A -o json | jq -r '.items[] | select(.metadata.annotations["secret-operator.example.com/expires-at"] != null) | "\(.metadata.namespace)/\(.metadata.name): expires \(.metadata.annotations["secret-operator.example.com/expires-at"])"'

# =============================================================================
# Development Helpers
# =============================================================================

.PHONY: logs
logs: ## Show recent events related to secrets
	@echo "$(GREEN)Recent Secret-related events:$(NC)"
	kubectl get events --field-selector involvedObject.kind=Secret --sort-by='.lastTimestamp'

.PHONY: watch-secrets
watch-secrets: ## Watch secrets in real-time
	kubectl get secrets -A -w

.PHONY: help
help: ## Show this help message
	@echo "Secret Rotation Operator - Available Commands"
	@echo "=============================================="
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
