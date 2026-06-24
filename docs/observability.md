# Observability Guide

This guide covers enabling distributed tracing, logs, and metrics for Claw instances using the Red Hat build of OpenTelemetry and Tempo.

The operator injects the OTel SDK configuration into the OpenClaw gateway pod via standard `OTEL_*` environment variables and `diagnostics.otel` config keys. Telemetry is sent directly from the gateway to an externally-deployed OTel Collector, which routes signals to your chosen backends.

## Prerequisites

The following operators must be installed cluster-wide before enabling observability on a Claw instance.

### Install cert-manager

The OTel Operator requires cert-manager for its admission webhooks.

```sh
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-cert-manager-operator
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-cert-manager-operator
  namespace: openshift-cert-manager-operator
spec:
  targetNamespaces:
  - openshift-cert-manager-operator
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: openshift-cert-manager-operator
  namespace: openshift-cert-manager-operator
spec:
  channel: stable-v1
  name: openshift-cert-manager-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

### Install the Red Hat build of OpenTelemetry Operator

```sh
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-opentelemetry-operator
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-opentelemetry-operator
  namespace: openshift-opentelemetry-operator
spec: {}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: opentelemetry-product
  namespace: openshift-opentelemetry-operator
spec:
  channel: stable
  name: opentelemetry-product
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

### Install the Tempo Operator

```sh
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-tempo-operator
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-tempo-operator
  namespace: openshift-tempo-operator
spec: {}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: tempo-product
  namespace: openshift-tempo-operator
spec:
  channel: stable
  name: tempo-product
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

Wait for all three to reach `Succeeded`:

```sh
oc get csv -n openshift-cert-manager-operator
oc get csv -n openshift-opentelemetry-operator
oc get csv -n openshift-tempo-operator
```

## Deploy the Observability Stack

These resources are deployed once per namespace where Claw instances run. All examples use `$NS` as the target namespace.

```sh
export NS=my-claw-namespace
```

### Deploy TempoMonolithic (trace storage)

For development and testing, use the in-memory backend. For production, configure S3-compatible object storage.

```sh
cat <<EOF | oc apply -f -
apiVersion: tempo.grafana.com/v1alpha1
kind: TempoMonolithic
metadata:
  name: tempo
  namespace: $NS
spec:
  storage:
    traces:
      backend: memory   # use s3/gcs/azure for production
  jaegerui:
    enabled: true
  resources:
    total:
      limits:
        memory: 1Gi
        cpu: 500m
EOF
```

### Grant RBAC for the OTel Collector

The `k8s_attributes` processor enriches spans with pod/namespace/deployment metadata and needs read access to those resources.

```sh
cat <<EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: otel-collector-k8sattributes-$NS
rules:
- apiGroups: [""]
  resources: ["pods", "namespaces"]
  verbs: ["get", "watch", "list"]
- apiGroups: ["apps"]
  resources: ["replicasets"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: otel-collector-k8sattributes-$NS
subjects:
- kind: ServiceAccount
  name: otel-collector
  namespace: $NS
roleRef:
  kind: ClusterRole
  name: otel-collector-k8sattributes-$NS
  apiGroup: rbac.authorization.k8s.io
EOF
```

### Deploy the OTel Collector

The collector receives OTLP from the gateway on port 4318, enriches spans with Kubernetes metadata, and forwards traces to Tempo.

```sh
cat <<EOF | oc apply -f -
apiVersion: opentelemetry.io/v1beta1
kind: OpenTelemetryCollector
metadata:
  name: otel
  namespace: $NS
spec:
  mode: deployment
  config:
    receivers:
      otlp:
        protocols:
          http:
            endpoint: 0.0.0.0:4318
          grpc:
            endpoint: 0.0.0.0:4317
    processors:
      memory_limiter:
        check_interval: 1s
        limit_percentage: 75
        spike_limit_percentage: 15
      batch:
        send_batch_size: 10000
        timeout: 10s
      k8s_attributes:
        auth_type: serviceAccount
        passthrough: false
        extract:
          metadata:
            - k8s.pod.name
            - k8s.namespace.name
            - k8s.deployment.name
    exporters:
      otlp_grpc:
        endpoint: tempo-tempo.$NS.svc:4317
        tls:
          insecure: true
      debug:
        verbosity: basic
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [memory_limiter, k8s_attributes, batch]
          exporters: [otlp_grpc, debug]
        logs:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [debug]
EOF
```

Verify everything is running:

```sh
oc get pods -n $NS | grep -E "otel|tempo"
# Expected:
# otel-collector-<hash>   1/1   Running
# tempo-tempo-0           3/3   Running
```

## Enable Observability on a Claw Instance

Add `traces`, `logs`, and/or `metrics` to the Claw spec, pointing `traces.endpoint` at the OTel Collector service:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: my-claw
  namespace: my-claw-namespace
spec:
  credentials:
  - name: openai
    provider: openai
    secretRef:
    - key: api-key
      name: my-llm-key

  traces:
    enabled: true
    endpoint: http://otel-collector.my-claw-namespace.svc:4318
    samplingRatio: "1"   # 0.0–1.0; default 1 (100%)

  logs:
    enabled: true
    # endpoint defaults to traces.endpoint when omitted

  metrics:
    enabled: true
    port: 9464           # Prometheus scrape port; default 9464
    serviceMonitor:
      enabled: true      # creates a Prometheus Operator ServiceMonitor
      interval: 30s
```

### What the operator injects

When any signal is enabled, the operator automatically:

- Sets `OTEL_SERVICE_NAME=openclaw` on the gateway container
- Sets `OTEL_EXPORTER_OTLP_ENDPOINT` to `spec.traces.endpoint`
- Sets `OTEL_RESOURCE_ATTRIBUTES` with `service.instance.id` (pod name) and `k8s.namespace.name`
- When traces are enabled: sets `OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG`
- Sets `diagnostics.otel.{traces,logs,metrics,endpoint}` in `operator.json` for the OpenClaw plugin system

The gateway pod will have **no OTel Collector sidecar** — telemetry goes directly to the external collector.

### Signals independently controlled

Each signal can be enabled without the others:

```yaml
# traces only
spec:
  traces:
    enabled: true
    endpoint: http://otel-collector.$NS.svc:4318

# metrics only (Prometheus scraping via ServiceMonitor)
spec:
  metrics:
    enabled: true
```

### Custom sampling

```yaml
spec:
  traces:
    enabled: true
    endpoint: http://otel-collector.$NS.svc:4318
    samplingRatio: "0.1"   # sample 10% of traces
```

### Separate logs endpoint

If your logs backend uses a different OTLP endpoint from your traces backend:

```yaml
spec:
  traces:
    enabled: true
    endpoint: http://tempo-collector.$NS.svc:4318
  logs:
    enabled: true
    endpoint: http://loki-collector.$NS.svc:4318
```

## Verify the Pipeline

### 1. Confirm the gateway has no sidecar

After reconciliation the gateway pod should have only one container:

```sh
oc get pod -n $NS -l app=claw,claw.sandbox.redhat.com/instance=<claw-name> \
  -o jsonpath='{.items[0].spec.containers[*].name}'
# Expected: gateway
```

### 2. Confirm OTEL env vars are set

```sh
oc get deployment <claw-name> -n $NS \
  -o jsonpath='{.spec.template.spec.containers[0].env}' \
  | python3 -c "
import json, sys
for e in json.load(sys.stdin):
    if 'OTEL' in e['name']:
        print(e['name'], '=', e.get('value', '<valueFrom>'))
"
# Expected:
# OTEL_SERVICE_NAME = openclaw
# OTEL_RESOURCE_ATTRIBUTES = service.instance.id=$(POD_NAME),k8s.namespace.name=$(POD_NAMESPACE)
# OTEL_EXPORTER_OTLP_ENDPOINT = http://otel-collector.<namespace>.svc:4318
# OTEL_TRACES_SAMPLER = parentbased_traceidratio
# OTEL_TRACES_SAMPLER_ARG = 1
```

### 3. Confirm diagnostics config in operator.json

```sh
oc get cm <claw-name>-config -n $NS -o json \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
cfg = json.loads(d['data']['operator.json'])
print(json.dumps(cfg.get('diagnostics', {}), indent=2))
"
# Expected:
# {
#   "otel": {
#     "endpoint": "http://otel-collector.<namespace>.svc:4318",
#     "traces": true,
#     "logs": true,
#     "metrics": true
#   }
# }
```

### 4. Send a test trace and verify it reaches Tempo

Port-forward the OTel Collector, send a synthetic span, then query Tempo to confirm the full pipeline:

```sh
# Terminal 1 — port-forward the collector
oc port-forward -n $NS svc/otel-collector 4318:4318

# Terminal 2 — send a test trace (traceId must be 32 hex chars, spanId 16)
TRACE_ID="aabbccddeeff00112233445566778899"
SPAN_ID="aabbccddeeff0011"

curl -s -o /dev/null -w "%{http_code}" \
  -H "Content-Type: application/json" \
  -d "{
    \"resourceSpans\": [{
      \"resource\": {\"attributes\": [
        {\"key\": \"service.name\", \"value\": {\"stringValue\": \"claw-otel-test\"}}
      ]},
      \"scopeSpans\": [{
        \"spans\": [{
          \"traceId\": \"$TRACE_ID\",
          \"spanId\": \"$SPAN_ID\",
          \"name\": \"verify-pipeline\",
          \"kind\": 2,
          \"startTimeUnixNano\": \"$(date +%s)000000000\",
          \"endTimeUnixNano\": \"$(( $(date +%s) + 1 ))000000000\",
          \"status\": {}
        }]
      }]
    }]
  }" \
  http://localhost:4318/v1/traces
# Expected: 200
```

Wait ~15 seconds for the batch processor to flush, then query Tempo:

```sh
# Terminal 1 — port-forward Tempo instead
oc port-forward -n $NS svc/tempo-tempo 3200:3200

# Terminal 2 — retrieve the trace by ID
curl -s "http://localhost:3200/api/traces/$TRACE_ID" \
  | python3 -m json.tool | grep -E '"name"|service.name'
# Expected:
# "service.name"
# "stringValue": "claw-otel-test"
# "name": "verify-pipeline"
```

### 5. Check the OTel Collector logs

```sh
oc logs -n $NS deployment/otel-collector --tail=20 | grep -E "Traces|Logs|spans|ready"
# On receipt of the test trace:
# info  Traces  {..., "resource spans": 1, "spans": 1}
# On startup (clean):
# info  service  Everything is ready. Begin running and processing data.
```

### 6. Check the Tempo tags index

After traces arrive, Tempo indexes their attributes. Confirm `service.name` and Kubernetes labels are present:

```sh
# with port-forward to Tempo on 3200 still active
curl -s http://localhost:3200/api/search/tags | python3 -m json.tool
# Expected tagNames to include: service.name, k8s.namespace.name, k8s.pod.name
```

## Accessing the Tempo UI

TempoMonolithic ships with a Jaeger-compatible query UI. Expose it via a Route:

```sh
oc expose svc/tempo-tempo-jaegerui -n $NS --port=16686
oc get route tempo-tempo-jaegerui -n $NS
```

Navigate to the route URL and search by `Service Name: openclaw` to see traces from the gateway.

## Architecture

```
Gateway pod                OTel Collector (deployment)      Tempo
┌──────────────┐           ┌────────────────────────────┐   ┌──────────┐
│   gateway    │  OTLP/    │  receivers: otlp (4317/18) │   │          │
│  container   │──HTTP────▶│  processors:               │──▶│  traces  │
│              │  :4318    │    k8s_attributes           │   │  storage │
│ OTEL_EXPORTER│           │    memory_limiter           │   │          │
│ _OTLP_ENDPOI│           │    batch                    │   └──────────┘
│ NT=http://..│           │  exporters: otlp_grpc       │
└──────────────┘           └────────────────────────────┘
```

The operator's responsibility is limited to configuring the gateway pod — setting `OTEL_*` environment variables and `diagnostics.otel.*` in `operator.json`. The collector and trace storage are platform infrastructure deployed independently.

## Troubleshooting

**Traces not appearing in Tempo**

Check the batch timeout — the collector batches for up to 10 seconds before flushing. After sending a test trace, wait 15 seconds before querying.

**OTel Collector pod failing to start**

Check for missing RBAC — the `k8s_attributes` processor needs `get/list/watch` on `pods`, `namespaces`, and `replicasets`. Verify the ClusterRoleBinding exists:

```sh
oc get clusterrolebinding otel-collector-k8sattributes-$NS
```

**Gateway cannot reach the OTel Collector**

The operator automatically adds a NetworkPolicy egress rule for the collector endpoint. Verify it is present:

```sh
oc get networkpolicy <claw-name>-egress -n $NS -o json \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
for r in d['spec'].get('egress', []):
    for p in r.get('ports', []):
        if p.get('port') == 4318:
            print('port 4318 rule present:', r)
"
```

If the endpoint is in a different namespace, ensure the `to` selector covers it — the operator generates a namespace-scoped rule for in-cluster endpoints. For endpoints in another namespace, use `spec.network.additionalEgress` to add the rule manually.

**`injectTelemetryEgressRules` skips the endpoint**

The operator classifies `*.svc` and `*.svc.cluster.local` hostnames as in-cluster and adds a pod/namespace selector rule. Bare IP addresses or external FQDNs get a port-only rule. If using an IP address for the collector, confirm the egress NetworkPolicy has the port open.

**Logs not forwarded**

The default logs pipeline in the example collector config uses only the `debug` exporter. To forward logs to a backend (e.g., Loki), add an `otlp_http` or `loki` exporter to the collector config and include it in the `logs` pipeline.
