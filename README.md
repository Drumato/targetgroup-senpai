# targetgroup-senpai

A Kubernetes controller that automatically manages AWS ELBv2 Target Groups for NodePort services. When a NodePort service is labeled with specific labels, targetgroup-senpai creates and manages corresponding AWS Target Groups, automatically registering and deregistering Kubernetes nodes as targets.

## Features

- **Automatic Target Group Management**: Creates, updates, and deletes AWS Target Groups based on Kubernetes NodePort services
- **Label-based Service Discovery**: Uses Kubernetes labels to identify services that should be managed
- **Smart Node Registration**: Handles both cluster-wide and local traffic policies (`ExternalTrafficPolicy`)
- **Continuous Synchronization**: Monitors node health and automatically updates target registrations
- **Orphan Cleanup**: Removes Target Groups when corresponding Kubernetes services are deleted
- **Dry Run Mode**: Test configurations without making actual AWS changes
- **Configurable Logging**: Structured logging with multiple levels (debug, info, warn, error)
- **Secure Deployment**: Runs with non-root user in distroless container

## How It Works

targetgroup-senpai operates as a Kubernetes controller that:

1. **Discovers Services**: Watches for NodePort services with the matching label (default: `app.kubernetes.io/managed-by=targetgroup-senpai`)
2. **Creates Target Groups**: Automatically creates AWS ELBv2 Target Groups for each discovered service
3. **Registers Targets**: Adds Kubernetes node IPs as targets in the Target Groups
4. **Handles Traffic Policies**:
   - **Cluster**: Registers all ready nodes as targets
   - **Local**: Only registers nodes that have pods matching the service selector
5. **Maintains State**: Continuously monitors and syncs the state between Kubernetes and AWS
6. **Cleanup**: Removes Target Groups when services are deleted or no longer match the labels

## Prerequisites

### Kubernetes Cluster
- Kubernetes 1.20+ (tested with controller-runtime v0.22.4)
- NodePort services that need Target Group integration
- Cluster nodes running on AWS EC2 instances

### AWS Requirements
- ELBv2 (Application/Network Load Balancer) service access
- VPC where Target Groups will be created
- Appropriate IAM permissions (see [IAM Permissions](#iam-permissions))

### IAM Permissions

The service account or EC2 instance role needs the following permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancing:CreateTargetGroup",
        "elasticloadbalancing:DeleteTargetGroup",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeTags",
        "elasticloadbalancing:ModifyTargetGroup",
        "elasticloadbalancing:RegisterTargets",
        "elasticloadbalancing:DeregisterTargets"
      ],
      "Resource": "*"
    }
  ]
}
```

## Installation

### Using Helm (Recommended)

The easiest way to deploy targetgroup-senpai is using the official Helm chart:

```bash
# Add the helm repository
helm repo add drumato https://drumato.github.io/helm-charts
helm repo update

# Install targetgroup-senpai
helm install targetgroup-senpai drumato/targetgroup-senpai \
  --namespace targetgroup-senpai \
  --create-namespace \
  --set config.vpcId="vpc-xxxxxxxxx"
```

For more advanced configurations and values, see the [helm-charts repository](https://github.com/Drumato/helm-charts).

### Manual Deployment

If you prefer to deploy manually, you can use the container image:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: targetgroup-senpai
  namespace: targetgroup-senpai
spec:
  replicas: 1
  selector:
    matchLabels:
      app: targetgroup-senpai
  template:
    metadata:
      labels:
        app: targetgroup-senpai
    spec:
      serviceAccountName: targetgroup-senpai
      containers:
      - name: targetgroup-senpai
        image: ghcr.io/drumato/targetgroup-senpai:latest
        env:
        - name: VPC_ID
          value: "vpc-xxxxxxxxx"
        - name: LOG_LEVEL
          value: "info"
        resources:
          limits:
            cpu: 100m
            memory: 128Mi
          requests:
            cpu: 50m
            memory: 64Mi
```

## Configuration

targetgroup-senpai is configured via environment variables:

| Environment Variable | Description | Default | Required |
|---------------------|-------------|---------|----------|
| `VPC_ID` | AWS VPC ID where Target Groups will be created | - | **Yes** |
| `MATCHING_LABEL_KEY` | Label key to identify managed services | `app.kubernetes.io/managed-by` | No |
| `MATCHING_LABEL_VALUE` | Label value to identify managed services | `targetgroup-senpai` | No |
| `INTERVAL_SECONDS` | Controller reconciliation interval | `60` | No |
| `LOG_LEVEL` | Logging level (`debug`, `info`, `warn`, `error`) | `info` | No |
| `DRY_RUN` | Enable dry-run mode (no AWS changes) | `false` | No |
| `CLIENT_TIMEOUT_SECONDS` | Timeout for Kubernetes/AWS API calls | `10` | No |
| `MIN_INSTANCE_COUNT` | Minimum nodes required for Target Group creation | `3` | No |
| `DELETE_ORPHAN_TARGET_GROUPS` | Enable deletion of orphaned Target Groups | `true` | No |

### AWS Configuration

targetgroup-senpai uses the AWS SDK's default credential chain:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. AWS profiles (`AWS_PROFILE`)
3. IAM roles (for EKS/EC2)
4. Instance metadata service

## Usage

### Basic Service Configuration

Label your NodePort services to enable Target Group management:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-app
  namespace: default
  labels:
    app.kubernetes.io/managed-by: targetgroup-senpai
spec:
  type: NodePort
  ports:
  - port: 80
    targetPort: 8080
    nodePort: 30080
  selector:
    app: my-app
```

### Advanced Configuration with Local Traffic Policy

For services that should only receive traffic on nodes with matching pods:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-local-app
  namespace: production
  labels:
    app.kubernetes.io/managed-by: targetgroup-senpai
spec:
  type: NodePort
  externalTrafficPolicy: Local  # Only nodes with pods will be registered
  ports:
  - port: 80
    targetPort: 8080
    nodePort: 30081
  selector:
    app: my-local-app
```

### Custom Label Configuration

If you want to use different labels for service discovery:

```bash
# Deploy with custom labels
helm install targetgroup-senpai drumato/targetgroup-senpai \
  --set config.matchingLabelKey="my-company.com/managed-by" \
  --set config.matchingLabelValue="my-load-balancer"
```

Then label your services accordingly:

```yaml
metadata:
  labels:
    my-company.com/managed-by: my-load-balancer
```

### Disabling Orphan Target Group Cleanup

By default, targetgroup-senpai automatically deletes Target Groups when their corresponding NodePort services are removed. You can disable this behavior:

```bash
# Deploy with orphan cleanup disabled
helm install targetgroup-senpai drumato/targetgroup-senpai \
  --set config.deleteOrphanTargetGroups=false
```

Or using environment variables:

```yaml
env:
- name: DELETE_ORPHAN_TARGET_GROUPS
  value: "false"
```

When disabled, Target Groups will remain in AWS even after their corresponding services are deleted. This can be useful for:
- Preventing accidental deletion of Target Groups
- Maintaining Target Groups for services that are temporarily removed
- Managing Target Group lifecycle manually

## Target Group Naming

Target Groups are created with the following naming convention:

- **Format**: `tgs-<namespace>-<service-name>`
- **Length Limit**: Truncated to 32 characters (AWS limit)
- **Examples**:
  - Service `my-app` in `default` namespace → `tgs-default-my-app`
  - Service `user-service` in `production` namespace → `tgs-production-user-service`

## Monitoring and Troubleshooting

### Logging

targetgroup-senpai provides structured logging. Enable debug logging for detailed information:

```yaml
env:
- name: LOG_LEVEL
  value: "debug"
```

### Dry Run Mode

Test your configuration without making AWS changes:

```yaml
env:
- name: DRY_RUN
  value: "true"
```

### Common Issues

#### Target Group Creation Fails
- Verify VPC_ID is correct and the VPC exists
- Check IAM permissions for ELBv2 operations
- Ensure node count meets MIN_INSTANCE_COUNT requirement

#### Nodes Not Registered as Targets
- Verify nodes are in "Ready" state
- For `externalTrafficPolicy: Local`, ensure pods exist on nodes
- Check that node internal IPs are accessible from the VPC

#### Services Not Discovered
- Verify the service has the correct labels
- Confirm service type is NodePort
- Check MATCHING_LABEL_KEY and MATCHING_LABEL_VALUE configuration

### Health Checks

Monitor the controller logs for reconciliation cycles:

```bash
kubectl logs -n targetgroup-senpai deployment/targetgroup-senpai -f
```

## Development

### Building Locally

```bash
# Clone the repository
git clone https://github.com/Drumato/targetgroup-senpai.git
cd targetgroup-senpai

# Install dependencies
go mod download

# Run tests and build
make

# Build binary
go build -o ./bin/targetgroup-senpai .
```

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run tests with coverage
go test -v -cover ./...
```

### Local Development

```bash
# Set required environment variables
export VPC_ID="vpc-xxxxxxxxx"
export LOG_LEVEL="debug"
export DRY_RUN="true"

# Run locally (requires kubeconfig)
./bin/targetgroup-senpai
```

### Makefile Targets

- `make format` - Format Go code
- `make test` - Run tests
- `make build` - Build binary
- `make lint` - Run golangci-lint
- `make` - Run format, test, build, and lint

## Examples

### Complete Deployment Example

```yaml
# Namespace
apiVersion: v1
kind: Namespace
metadata:
  name: my-app

---
# Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: my-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: nginx
        ports:
        - containerPort: 80

---
# NodePort Service with targetgroup-senpai management
apiVersion: v1
kind: Service
metadata:
  name: my-app
  namespace: my-app
  labels:
    app.kubernetes.io/managed-by: targetgroup-senpai
spec:
  type: NodePort
  ports:
  - port: 80
    targetPort: 80
    nodePort: 30080
  selector:
    app: my-app
```

### Integration with Application Load Balancer

After targetgroup-senpai creates your Target Groups, you can attach them to an ALB:

```bash
# Find your Target Group ARN
aws elbv2 describe-target-groups --names "tgs-my-app-my-app"

# Create ALB listener rule (example)
aws elbv2 create-listener \
  --load-balancer-arn arn:aws:elasticloadbalancing:region:account:loadbalancer/app/my-alb/xxxxx \
  --protocol HTTP \
  --port 80 \
  --default-actions Type=forward,TargetGroupArn=arn:aws:elasticloadbalancing:region:account:targetgroup/tgs-my-app-my-app/xxxxx
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and linting (`make`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [helm-charts](https://github.com/Drumato/helm-charts) - Official Helm charts for deployment
- [AWS Load Balancer Controller](https://github.com/kubernetes-sigs/aws-load-balancer-controller) - Alternative approach for ALB/NLB integration