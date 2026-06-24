# Using OpenShell With The Claw Operator

This guide deploys an OpenClaw `Claw` that keeps provider and channel traffic
on the `claw-proxy` path while running agent exec sessions in OpenShell-managed
sandbox pods.

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

## Prerequisites

You need:

- a cluster with the `claw-operator` installed
- the updated `Claw` and `OpenShellGateway` CRDs installed
- `oc` or `kubectl`
- an OpenClaw image that contains the OpenShell CLI at
  `/opt/openshell/bin/openshell`
- an OpenShell sandbox image reachable by the cluster
- provider credentials stored as Kubernetes Secrets in the OpenClaw namespace

The OpenShell Kubernetes driver uses the Agent Sandbox API. Install its CRDs
once per cluster:

```shell
AGENT_SANDBOX_REPO=https://github.com/kubernetes-sigs/agent-sandbox
AGENT_SANDBOX_RELEASE="${AGENT_SANDBOX_REPO}/releases/latest/download"
kubectl apply -f "${AGENT_SANDBOX_RELEASE}/manifest.yaml"
kubectl get crd sandboxes.agents.x-k8s.io
```

On OpenShift, the current OpenShell sandbox path needs privileged SCC for the
sandbox ServiceAccount. Keep that grant in the OpenShell namespace, not the
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

## Deploy An OpenShell Gateway

Create the OpenShell namespace:

```shell
oc new-project openshell-alice
```

Create an `OpenShellGateway` in that namespace:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: OpenShellGateway
metadata:
  name: openshell
  namespace: openshell-alice
spec:
  sandboxImage: quay.io/<org>/openclaw-openshell-sandbox:latest
  openShift:
    privilegedSandboxSCC: true
```

Apply it:

```shell
oc apply -f openshellgateway.yaml
oc wait -n openshell-alice openshellgateway/openshell \
  --for=condition=Ready \
  --timeout=180s
oc get openshellgateway -n openshell-alice
```

The operator reports the in-cluster endpoint in `.status.endpoint`, typically:

```text
http://openshell.openshell-alice.svc.cluster.local:8080
```

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
    gatewayRef:
      name: openshell
      namespace: openshell-alice
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

If the OpenShell gateway is not ready yet, the `Claw` reports
`OpenShellConfigured=False` with reason `Provisioning` and reconciles again.

## What The Operator Configures

When `spec.openshell.enabled` is true, the operator:

1. installs the `@openclaw/openshell-sandbox` OpenClaw runtime plugin
2. writes OpenShell sandbox defaults into `operator.json`
3. points the plugin at the referenced OpenShell gateway endpoint
4. sets the sandbox image passed to OpenShell
5. adds `NO_PROXY` entries for the OpenShell gateway Service
6. adds NetworkPolicy egress from the Claw gateway to the OpenShell gateway
7. uses `spec.openshell.openClawImage` for the OpenClaw gateway and init images

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

## Direct Endpoint Option

If the OpenShell gateway was not created by an `OpenShellGateway` CR, use a
direct in-cluster endpoint instead:

```yaml
spec:
  openshell:
    enabled: true
    gatewayEndpoint: http://openshell.openshell-alice.svc.cluster.local:8080
    openClawImage: quay.io/<org>/openclaw:openshell
    sandboxImage: quay.io/<org>/openclaw-openshell-sandbox:latest
```

The endpoint must use in-cluster Service DNS so the operator can derive
`NO_PROXY` hosts and a NetworkPolicy egress target.

## Troubleshooting

If the `OpenShellGateway` CR exists but no pods appear, check operator logs and
RBAC. The operator must be running with the OpenShellGateway controller and RBAC
that can create Services, Deployments, Roles, RoleBindings, ClusterRoles, and
Agent Sandbox resources.

If the Claw reports `OpenShellConfigured=False`, inspect the condition message:

```shell
oc get claw alice -n openclaw-alice -o yaml
```

If agent exec reports `spawn /opt/openshell/bin/openshell ENOENT`, the
`openClawImage` does not contain the OpenShell CLI at the expected path.

If the agent hangs while creating a sandbox, check:

- the Claw gateway can reach the OpenShell gateway Service
- the generated NetworkPolicy allows that egress
- `NO_PROXY` includes the OpenShell Service DNS names
- the OpenShell namespace has the required OpenShift SCC setup
- sandbox pods can pull `spec.openshell.sandboxImage`
