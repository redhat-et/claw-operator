# OpenClaw OpenShell Sandbox

The OpenClaw Operator keeps the OpenClaw gateway as the persistent application
boundary where Kubernetes policy, NetworkPolicy, RBAC, credential proxying, and
operator-reconciled configuration are applied. Agent session execution is
delegated to OpenShell through the OpenClaw OpenShell Sandbox plugin, a verified
plugin maintained within the OpenClaw codebase.

This isolates the riskiest runtime activity without moving the whole control
plane, credentials, UI, and user state into the sandbox runtime. Today,
OpenShell sandbox pods are expected to run in a separate namespace because they
require privileged SCC on OpenShift. If OpenShell no longer requires that
privilege, the same model could place sandbox pods in the Claw namespace while
preserving the logical separation between the gateway and per-session execution
pods.

This document describes the intended security and network boundaries when a
Claw instance uses the OpenShell Sandbox plugin. With this plugin, the OpenClaw
gateway uses the claw-operator managed credential proxy. This keeps the
credential and L7 provider-control boundary in the existing proxy while moving
agent execution into OpenShell-managed sandbox pods.

This direct-endpoint integration assumes an OpenShell gateway already exists. The
claw-operator only configures OpenClaw to call that endpoint; it does not deploy
or manage the OpenShell-side control plane, gateway, sandbox CRDs, SCC grants,
RBAC, or sandbox pod lifecycle.

## Assumptions

- The Claw gateway still runs with `claw-proxy` enabled for provider and channel
  traffic.
- OpenShell is used only as the agent session execution backend.
- The OpenShell gateway runs in a separate namespace, for example
  `openshell-alice`.
- OpenShell sandbox pods are created in the OpenShell namespace, not in the
  Claw namespace.
- `spec.openshell.gatewayEndpoint` points to an existing in-cluster OpenShell
  gateway Service with a non-empty selector.
- The OpenClaw image contains the OpenShell CLI plus the local tools required by
  the plugin, currently `ssh`.

## Traffic Boundaries

```text
LLM and channel traffic:
OpenClaw gateway -> claw-proxy -> external provider or channel API

Agent shell and tool execution:
OpenClaw gateway -> OpenShell gateway -> OpenShell sandbox pod

Agent-originated network access:
OpenShell sandbox pod -> cluster or internet target
```

The proxy and OpenShell protect different parts of the system. `claw-proxy`
remains responsible for provider and channel credential injection, provider
domain allowlisting, and TLS MITM policy enforcement. OpenShell is responsible
for separating agent-executed shell commands from the OpenClaw gateway process
and its mounted Kubernetes Secrets.

## Secret Boundary

Provider and channel credentials should remain in Kubernetes Secrets referenced
by the Claw CR. The operator mounts or references those Secrets only in the
proxy path. The OpenShell sandbox pod should not receive provider API keys by
default.

With this model, an agent can run commands in the OpenShell sandbox, but those
commands do not automatically inherit the OpenClaw gateway's provider secrets.
If a sandboxed agent needs a separate secret, that should be modeled as an
explicit capability with a scoped Secret, not inherited from the gateway.

## NetworkPolicy Boundary

The existing `{instance}-egress` policy allows the OpenClaw gateway pod to reach
only `claw-proxy` and DNS. When OpenShell is enabled, the operator must also
allow the gateway pod to reach the OpenShell gateway Service on its configured
port, usually TCP `8080`.

Kubernetes NetworkPolicy cannot allow egress by DNS name. The operator should
generate a selector-based rule targeting the OpenShell namespace and gateway pod
labels, for example:

```yaml
egress:
  - to:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: openshell-alice
        podSelector:
          matchLabels:
            app.kubernetes.io/name: openshell
            app.kubernetes.io/instance: openshell
    ports:
      - protocol: TCP
        port: 8080
```

The OpenShell gateway endpoint must also bypass `claw-proxy` with `NO_PROXY`;
otherwise in-cluster OpenShell control traffic may be sent through the
credential proxy by mistake. This bypass should be specific to the OpenShell
gateway endpoint instead of enabling broad in-cluster bypass unless the user
explicitly requests broad bypass.

For direct endpoints, the endpoint must name an in-cluster Service with a
non-empty `spec.selector`; the operator uses that selector as the NetworkPolicy
egress target.

## Sandbox Egress

OpenShell sandbox pod egress is controlled by policies in the OpenShell
namespace and any cluster-level OpenShift/CNI egress controls. It is not
controlled by `claw-proxy` unless the sandbox is explicitly configured to use a
proxy.

This means a request such as "download this file" runs from the OpenShell
sandbox pod. It is blocked only if the OpenShell namespace or cluster has an
egress policy that blocks it. Without an egress policy selecting the sandbox
pod, Kubernetes generally allows egress by default.

For a secure default, the OpenShell namespace should use default-deny egress for
sandbox pods and then add only the minimum rules required for OpenShell control
traffic, DNS if needed, and any cluster-admin-approved outbound destinations.

## Claw-Operator Responsibilities

When `spec.openshell` is enabled, the claw-operator:

1. Configure OpenClaw to use the OpenShell sandbox plugin.
2. Configure the OpenShell gateway endpoint from
   `spec.openshell.gatewayEndpoint`.
3. Optionally override the OpenClaw image with
   `spec.openshell.openClawImage`, which should include the OpenShell CLI and
   plugin runtime dependencies.
4. Add gateway egress to the OpenShell gateway Service.
5. Add the OpenShell gateway host to `NO_PROXY`.
6. Keep provider and channel traffic routed through `claw-proxy`.
7. Default the plugin to OpenShell `remote` mode unless mirror synchronization
   is fixed and validated.

It does not reconcile OpenShell-side infrastructure. The OpenShell deployment
must provide the gateway Service, sandbox API dependencies, OpenShift SCC/RBAC,
and any namespace egress policy before the `Claw` references it.

This keeps the credential and L7 provider-control boundary in the existing proxy
while moving agent execution into OpenShell-managed sandbox pods.
