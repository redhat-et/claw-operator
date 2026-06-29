## Context

The gateway Deployment currently uses the namespace's default ServiceAccount with `automountServiceAccountToken` implicitly false (Kubernetes default for pods without an explicit SA). The operator does not set either field. This is intentional — the agent should not have a Kubernetes API token by default, since it could use it to escalate privileges or discover cluster resources.

However, legitimate use cases exist for mounting a SA token: Workload Identity (GCP, AWS), in-cluster MCP servers that need API access, and agents that interact with Kubernetes resources through curated RBAC.

## Goals / Non-Goals

**Goals:**
- Let users assign a specific ServiceAccount to the gateway pod
- Automatically enable token mounting when a custom SA is set
- Keep the default behavior unchanged (no token, default SA)

**Non-Goals:**
- Creating the ServiceAccount itself (that's the admin's responsibility)
- Validating that the referenced ServiceAccount exists (Kubernetes handles this — the pod stays Pending if the SA doesn't exist)
- Configuring RBAC for the ServiceAccount

## Decisions

### Automatically enable automountServiceAccountToken

When `spec.serviceAccountName` is set, the operator also sets `automountServiceAccountToken: true` on the pod template. Without this, the SA is assigned but no token is projected, which defeats the purpose in every known use case (Workload Identity, API access).

**Why:** Setting a custom SA without mounting the token is almost never intentional. Making the user set both fields independently would be error-prone and surprising.

**Alternative considered:** Add a separate `spec.automountServiceAccountToken` field. Rejected because it adds complexity for a scenario (custom SA without token) that has no known use case today.

### No-op when field is empty

When `spec.serviceAccountName` is omitted or empty, the function returns immediately without modifying the Deployment. The pod uses the default SA with no token mount (existing behavior).

**Why:** Zero risk of changing behavior for existing manifests.

### Target only the gateway Deployment

The SA is set only on the gateway Deployment, not on the proxy Deployment. The proxy does not need Kubernetes API access — it handles HTTP credential injection only.

**Why:** Principle of least privilege. The proxy's security boundary is strengthened by having no SA token.

## Risks / Trade-offs

- **[Security surface]** Mounting a SA token gives the agent access to whatever RBAC the SA has. This is the user's responsibility — the operator does not audit or restrict SA permissions. Documentation should emphasize creating narrowly-scoped SAs.
- **[No SA existence check]** If the user references a non-existent SA, the pod stays in Pending state. The operator does not surface this as a status condition. This matches standard Kubernetes behavior for SA references.
