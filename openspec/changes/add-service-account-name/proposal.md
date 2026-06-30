## Why

The gateway pod runs with the namespace's default ServiceAccount and `automountServiceAccountToken` disabled. This prevents the agent from accessing the Kubernetes API, which is the safe default. However, some use cases require in-cluster API access — for example, an agent that queries cluster status via an MCP server, or a pod that needs GCP Workload Identity or AWS IRSA (both implemented via projected SA tokens). Today there is no way to assign a custom ServiceAccount without manually patching the Deployment after the operator reconciles it.

## What Changes

- Add an optional `spec.serviceAccountName` field to the Claw CRD
- When set, the operator sets the ServiceAccount on the gateway pod template and enables `automountServiceAccountToken` so the SA token is projected
- When omitted, existing behavior is preserved (default SA, no token mounted)

## Capabilities

### New Capabilities

- `gateway-service-account`: Assign a custom Kubernetes ServiceAccount to the gateway pod via `spec.serviceAccountName`, with automatic token mounting

### Modified Capabilities

- `claw-crd`: Add `spec.serviceAccountName` field to ClawSpec

## Impact

- `api/v1alpha1/claw_types.go` — add ServiceAccountName field to ClawSpec
- `internal/controller/claw_deployment.go` — add function to set serviceAccountName and automountServiceAccountToken on the gateway Deployment
- `internal/controller/claw_resource_controller.go` — call the new function during reconciliation
- CRD manifest regeneration
- No breaking changes, no migration needed
