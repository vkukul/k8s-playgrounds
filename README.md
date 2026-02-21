# Secret Rotation Operator

A Kubernetes controller written in Go that monitors Secrets for expiration and generates alerts. This project is designed as a learning exercise to understand Go programming patterns and Kubernetes controller development using raw `client-go` no frameworks.

## Project Goals

- Learn Go best practices through handson development
- Understand Kubernetes controller patterns without framework abstraction
- Build a practical tool that monitors Secret expiration with annotations

## How It Works

The operator watches Kubernetes Secrets for a custom annotation:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-secret-key
  annotations:
    secret-operator.example.com/expires-at: "2024-12-31"
    secret-operator.example.com/warn-before: "7d"  # optional, default 7 days
type: Opaque
data:
  api-key: <base64-encoded-value>
```

When a Secret is approaching its expiration date, the operator:
1. Logs a warning message
2. Creates a Kubernetes Event on the Secret
3. Can trigger external alerts

## Prerequisites

Before you begin, ensure you have the following installed:

| Tool     | Version | Installation                                                              |
| -------- | ------- | ------------------------------------------------------------------------- |
| Go       | 1.21+   | [golang.org/dl](https://golang.org/dl/)                                   |
| minikube | 1.30+   | [minikube.sigs.k8s.io](https://minikube.sigs.k8s.io/docs/start/)          |
| kubectl  | 1.28+   | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/) |
| make     | any     | Usually pre-installed on macOS/Linux                                      |

### Verify Installation

```bash
# Check Go version
go version

# Check minikube version
minikube version

# Check kubectl version
kubectl version --client
```

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/yourusername/k8s-playgrounds.git
cd k8s-playgrounds
```

### 2. Start minikube

```bash
# Start a local Kubernetes cluster
make cluster-start

# Verify the cluster is running
make cluster-status
```

### 3. Build and Run the Operator

```bash
# Download dependencies
make deps

# Build the operator
make build

# Run the operator locally (connects to minikube)
make run
```

### 4. Test with a Sample Secret

```bash
# Create a test secret with expiration annotation
make create-test-secret

# Watch the operator logs for expiration warnings
```

## Development

### Project Structure

```
k8s-playgrounds/
├── README.md
├── Dockerfile                    # Multi-stage Docker build
├── Makefile                      # Build, test, deploy commands
├── go.mod
├── go.sum
├── cmd/
│   └── secret-operator/
│       └── main.go               # Application entry point
├── internal/
│   └── controller/
│       └── secret_controller.go  # Controller logic
├── deploy/                       # Kubernetes manifests
│   ├── namespace.yaml
│   ├── serviceaccount.yaml
│   ├── rbac.yaml
│   └── deployment.yaml
└── scripts/
    └── test-secrets.yaml         # Sample secrets for testing
```

### Common Commands

```bash
make help            # Show all available commands
make build           # Build the operator binary
make run             # Run the operator locally
make test            # Run unit tests
make docker-build    # Build Docker image
make docker-load     # Load image into minikube
make deploy          # Deploy to minikube cluster
make undeploy        # Remove from cluster
make deploy-logs     # Tail operator pod logs
make cluster-start   # Start minikube cluster
make cluster-stop    # Stop minikube cluster
```

## Learning Path

This project is structured as a learning exercise. Here's the progression:

### Phase 1: Project Setup & Cluster Connection
- [x] Set up Go module structure
- [x] Create Makefile for common tasks
- [x] Connect to Kubernetes cluster using client-go
- [x] Verify connection by listing namespaces

### Phase 2: Reading Secrets
- [x] List Secrets from the cluster
- [x] Parse custom annotations
- [x] Filter Secrets with expiration annotations

### Phase 3: Watching Resources
- [x] Implement SharedInformer for Secrets
- [x] Handle Add/Update/Delete events
- [x] Understand the cache and indexer

### Phase 4: Controller Logic
- [x] Implement work queue pattern
- [x] Build reconciliation loop
- [x] Check expiration dates and determine action

### Phase 5: Taking Action
- [x] Create Kubernetes Events for expiring secrets
- [x] Add structured logging
- [x] Implement graceful shutdown

### Phase 6: Containerize & Deploy
- [x] Create multi-stage Dockerfile
- [x] Add in-cluster config support
- [x] Create RBAC manifests (ServiceAccount, ClusterRole, ClusterRoleBinding)
- [x] Create Deployment manifest
- [x] Deploy to minikube and verify

## Deploying to Kubernetes

### Build and Load the Image

```bash
# Build the Docker image
make docker-build

# Load it into minikube (no registry needed)
make docker-load
```

### Deploy

```bash
# Apply all manifests (namespace, RBAC, deployment)
make deploy

# Check the operator is running
make deploy-status

# View logs
make deploy-logs
```

### Verify Events

```bash
# Create test secrets
make create-test-secret

# Check events created by the in-cluster operator
kubectl get events --field-selector reason=SecretExpired
kubectl describe secret expired-secret-key
```

### Cleanup

```bash
make undeploy
```

## Key Concepts

### What is a Kubernetes Controller?

A controller is a control loop that watches the state of your cluster and makes changes to move the current state toward the desired state. Our controller:

1. **Watches** - Observes Secret resources for changes
2. **Analyzes** - Checks if secrets have expiration annotations
3. **Acts** - Creates events/alerts for expiring secrets

### client-go Components We'll Use

| Component              | Purpose                                          |
| ---------------------- | ------------------------------------------------ |
| `kubernetes.Clientset` | Main client for interacting with Kubernetes API  |
| `SharedInformer`       | Efficiently watches resources with local caching |
| `Workqueue`            | Rate limited queue for processing events         |
| `EventRecorder`        | Creates Kubernetes Events                        |

## Troubleshooting

### minikube won't start

```bash
# Delete and recreate the cluster
minikube delete
minikube start
```

### Cannot connect to cluster

```bash
# Ensure minikube is running
minikube status

# Update kubeconfig
minikube update-context
```

### Go module issues

```bash
# Clean and re-download dependencies
go clean -modcache
make deps
```

## Resources

- [client-go Documentation](https://pkg.go.dev/k8s.io/client-go)
- [Kubernetes API Concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/)
- [Writing Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
- [Go by Example](https://gobyexample.com/)
