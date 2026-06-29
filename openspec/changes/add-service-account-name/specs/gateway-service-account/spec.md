## ADDED Requirements

### Requirement: ClawSpec has serviceAccountName field
The Claw CRD SHALL include an optional `spec.serviceAccountName` string field that sets the Kubernetes ServiceAccount on the gateway pod.

#### Scenario: Field is omitted
- **WHEN** a Claw is created without `spec.serviceAccountName`
- **THEN** the gateway pod SHALL use the namespace's default ServiceAccount
- **THEN** `automountServiceAccountToken` SHALL NOT be set on the pod template

#### Scenario: Field is set to a valid SA name
- **WHEN** a Claw is created with `spec.serviceAccountName: "my-agent-sa"`
- **THEN** the gateway pod template SHALL have `serviceAccountName: my-agent-sa`
- **THEN** `automountServiceAccountToken` SHALL be set to `true` on the pod template

#### Scenario: Field is set to empty string
- **WHEN** a Claw is created with `spec.serviceAccountName: ""`
- **THEN** the gateway pod SHALL use the namespace's default ServiceAccount
- **THEN** `automountServiceAccountToken` SHALL NOT be set on the pod template

### Requirement: Token mounting is automatic
When `spec.serviceAccountName` is set, the operator SHALL enable `automountServiceAccountToken: true` on the gateway pod template without requiring a separate field.

#### Scenario: SA token is projected
- **WHEN** a Claw has `spec.serviceAccountName: "my-agent-sa"` and the SA exists
- **THEN** the pod SHALL have a projected SA token volume mounted
- **THEN** workloads inside the pod SHALL be able to access the Kubernetes API using the SA's RBAC

#### Scenario: SA token is usable for Workload Identity
- **WHEN** a Claw has `spec.serviceAccountName` set to an SA annotated for GCP Workload Identity or AWS IRSA
- **THEN** the projected token SHALL be usable for cloud provider authentication

### Requirement: Only the gateway Deployment is affected
The operator SHALL set serviceAccountName only on the gateway Deployment, not on the proxy Deployment or any other managed resource.

#### Scenario: Proxy Deployment unchanged
- **WHEN** a Claw has `spec.serviceAccountName: "my-agent-sa"`
- **THEN** the proxy Deployment SHALL NOT have a custom ServiceAccount set
- **THEN** the proxy Deployment SHALL NOT have `automountServiceAccountToken: true`

### Requirement: Non-existent SA follows Kubernetes behavior
The operator SHALL NOT validate whether the referenced ServiceAccount exists. Kubernetes handles this natively — the pod stays in Pending state.

#### Scenario: SA does not exist
- **WHEN** a Claw has `spec.serviceAccountName: "nonexistent-sa"` and the SA does not exist
- **THEN** the operator SHALL still render the Deployment with the SA set
- **THEN** the pod SHALL remain in Pending state until the SA is created
