# Using OpenShell Through A Direct Gateway Endpoint

This guide deploys an OpenClaw `Claw` that keeps provider and channel traffic
on the `claw-proxy` path while running agent exec sessions in OpenShell-managed
sandbox pods through an existing OpenShell gateway endpoint.

For the security model and network boundary details, see
[openshell-security-boundaries.md](openshell-security-boundaries.md).

## Model

Use one OpenShell namespace and one OpenClaw namespace per human user:

```text
openshell-alice          # platform-owned OpenShell gateway and sandboxes
openclaw-alice           # user's Claw gateway, proxy, PVC, and config
```

OpenClaw is not itself run inside an OpenShell sandbox. The persistent Claw
gateway stays in the normal OpenClaw namespace. When an agent needs command
execution, OpenClaw calls the OpenShell sandbox plugin, which asks that user's
OpenShell gateway to create or reuse a sandbox pod.

This integration does not deploy, own, or reconcile the OpenShell gateway. The
`Claw` resource only names an existing in-cluster Service endpoint in
`spec.openshell.gatewayEndpoint`; the gateway deployment, namespace, SCC grants,
Agent Sandbox CRDs, and sandbox lifecycle remain outside the claw-operator.

## Prerequisites

You need:

- a cluster with the `claw-operator` installed
- the updated `Claw` CRD installed
- `oc` or `kubectl`
- a standalone OpenShell gateway already running in-cluster
- an OpenClaw image that contains the OpenShell CLI at
  `/opt/openshell/bin/openshell`
- an OpenShell sandbox image reachable by the cluster
- provider credentials stored as Kubernetes Secrets in the OpenClaw namespace

The OpenShell Kubernetes driver uses the Agent Sandbox API. If you own the
standalone OpenShell deployment, make sure its CRDs are installed once per
cluster:

```shell
AGENT_SANDBOX_REPO=https://github.com/kubernetes-sigs/agent-sandbox
AGENT_SANDBOX_RELEASE="${AGENT_SANDBOX_REPO}/releases/latest/download"
kubectl apply -f "${AGENT_SANDBOX_RELEASE}/manifest.yaml"
kubectl get crd sandboxes.agents.x-k8s.io
```

On OpenShift, the current OpenShell sandbox path needs privileged SCC for the
sandbox ServiceAccount. That grant belongs to the OpenShell namespace, not the
OpenClaw namespace.

## Build Images

Build and push the OpenClaw image with the OpenShell CLI:

```shell
OPENCLAW_IMAGE=quay.io/<org>/openclaw:openshell
./openshell-images/build-openclaw-source-image.sh "${OPENCLAW_IMAGE}"
podman push "${OPENCLAW_IMAGE}"
```

Build and push the sandbox image:

```shell
SANDBOX_IMAGE=quay.io/<org>/openclaw-openshell-sandbox:latest
./openshell-images/build-sandbox-image.sh "${SANDBOX_IMAGE}"
podman push "${SANDBOX_IMAGE}"
```

See [openshell-images/README.md](../openshell-images/README.md) for build args
and image contents.

## Locate An Existing OpenShell Gateway

This guide assumes the OpenShell gateway is deployed independently from the
claw-operator. The gateway must expose an in-cluster Service DNS endpoint that
the Claw gateway can reach.

For example, a standalone gateway Service in `openshell-alice` might be:

```shell
http://openshell.openshell-alice.svc.cluster.local:8080
```

The endpoint must use Service DNS, not an external route, and the Service must
define `spec.selector`. The operator uses the Service DNS name for `NO_PROXY`
and the Service selector for the NetworkPolicy egress target.

## Deploy A Claw With OpenShell

Create the OpenClaw namespace and provider Secret:

```shell
oc new-project openclaw-alice
oc create secret generic openai-api-key \
  -n openclaw-alice \
  --from-literal=api-key="${OPENAI_API_KEY}"
```

Create the `Claw`:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: alice
  namespace: openclaw-alice
spec:
  credentials:
    - name: openai
      provider: openai
      type: bearer
      secretRef:
        - name: openai-api-key
          key: api-key
  openshell:
    enabled: true
    gatewayEndpoint: http://openshell.openshell-alice.svc.cluster.local:8080
    openClawImage: quay.io/<org>/openclaw:openshell
    sandboxImage: quay.io/<org>/openclaw-openshell-sandbox:latest
    mode: remote
```

Apply it:

```shell
oc apply -f claw-openshell.yaml
oc wait -n openclaw-alice claw/alice \
  --for=condition=OpenShellConfigured \
  --timeout=180s
oc rollout status -n openclaw-alice deployment/alice
```

If `gatewayEndpoint` is missing or does not use in-cluster Service DNS, the
`Claw` reports `OpenShellConfigured=False` with reason `ValidationFailed`.

## What The Operator Configures

When `spec.openshell.enabled` is true, the operator:

1. installs the `@openclaw/openshell-sandbox` OpenClaw runtime plugin
2. writes OpenShell sandbox defaults into `operator.json`
3. points the plugin at `spec.openshell.gatewayEndpoint`
4. sets the sandbox image passed to OpenShell
5. adds `NO_PROXY` entries for the OpenShell gateway Service
6. adds NetworkPolicy egress from the Claw gateway to the OpenShell gateway
7. uses `spec.openshell.openClawImage` for the OpenClaw gateway and init images

The operator does not create OpenShell Deployments, Services, sandboxes,
Sandbox CRs, RBAC, SCC grants, or namespace policies for the OpenShell side.
Those must already be provided by the OpenShell deployment you point
`gatewayEndpoint` at.

Provider and channel credentials remain on the normal `claw-proxy` path. The
OpenShell sandbox pod should not receive provider API keys by default.

## Verify

Verify that the OpenClaw gateway has the OpenShell CLI:

```shell
oc exec -n openclaw-alice deployment/alice -c gateway -- \
  /opt/openshell/bin/openshell --version
```

Open the Claw UI and ask an agent to run an exec task, for example:

```text
Run `pwd`, write the output to sandbox-proof.txt, then read it back.
```

Watch the OpenShell namespace:

```shell
oc get pods -n openshell-alice -w
```

Inspect gateway logs:

```shell
oc logs -n openclaw-alice deployment/alice -c gateway -f
oc logs -n openshell-alice deployment/openshell -f
```

Expected result:

- the Claw gateway and proxy run in `openclaw-alice`
- OpenShell gateway and sandbox pods run in `openshell-alice`
- the agent command output returns in the Claw UI
- provider requests still flow through `claw-proxy`

## Troubleshooting

If the standalone OpenShell gateway has no pods, check the deployment mechanism
that owns that gateway. This operator only configures the Claw gateway to call
the endpoint in `spec.openshell.gatewayEndpoint`.

If the Claw reports `OpenShellConfigured=False`, inspect the condition message:

```shell
oc get claw alice -n openclaw-alice -o yaml
```

If agent exec reports `spawn /opt/openshell/bin/openshell ENOENT`, the
`openClawImage` does not contain the OpenShell CLI at the expected path.

If the agent hangs while creating a sandbox, check:

- the Claw gateway can reach the OpenShell gateway Service
- the OpenShell gateway Service has a non-empty `spec.selector`
- the generated NetworkPolicy allows that egress
- `NO_PROXY` includes the OpenShell Service DNS names
- the OpenShell namespace has the required OpenShift SCC setup
- sandbox pods can pull `spec.openshell.sandboxImage`
