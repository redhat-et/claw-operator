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

Multi-tenancy is required by the OpenShift Console distributed tracing UI plugin. Set `tenantName` to any label for your environment (e.g. `dev`). For production use S3-compatible object storage.

```sh
# Choose a tenant name — used in X-Scope-OrgID headers and RBAC rules
export TENANT=dev

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
  multitenancy:
    enabled: true
    mode: openshift
    authentication:
    - tenantName: $TENANT
      tenantId: "$(uuidgen | tr '[:upper:]' '[:lower:]')"
    resources:
      total:
        limits:
          memory: 500Mi
  resources:
    total:
      limits:
        memory: 1Gi
        cpu: 500m
EOF
```

### Grant RBAC for the OTel Collector

Two sets of RBAC are needed: one for the `k8s_attributes` processor (reads pod/namespace metadata) and one for writing traces to the Tempo tenant.

The Tempo gateway uses SubjectAccessReview to authorise writes. The required rule grants `create` on the `<tenantName>` resource within the `tempo.grafana.com` API group.

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
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tempo-traces-write-$TENANT
rules:
- apiGroups:
  - tempo.grafana.com
  resources:
  - tempomonolithics
  - "tempomonolithics/api/traces/v1/$TENANT"
  - $TENANT
  resourceNames:
  - tempo
  verbs:
  - get
  - create
  - update
- apiGroups:
  - tempo.grafana.com
  resources:
  - $TENANT
  verbs:
  - create
  - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tempo-traces-write-$TENANT-otel-collector
subjects:
- kind: ServiceAccount
  name: otel-collector
  namespace: $NS
roleRef:
  kind: ClusterRole
  name: tempo-traces-write-$TENANT
  apiGroup: rbac.authorization.k8s.io
EOF
```

### Deploy the OTel Collector

The collector receives OTLP from the gateway on port 4318, enriches spans with Kubernetes metadata, and forwards traces to Tempo through the multi-tenant gateway. It authenticates using the pod's service account token and the `X-Scope-OrgID` header identifies the tenant.

The Tempo gateway uses a certificate signed by the OpenShift service CA. The `openshift-service-ca.crt` ConfigMap is automatically injected into every namespace.

```sh
cat <<EOF | oc apply -f -
apiVersion: opentelemetry.io/v1beta1
kind: OpenTelemetryCollector
metadata:
  name: otel
  namespace: $NS
spec:
  mode: deployment
  volumes:
  - name: openshift-service-ca
    configMap:
      name: openshift-service-ca.crt
  volumeMounts:
  - name: openshift-service-ca
    mountPath: /etc/openshift-ca
    readOnly: true
  config:
    extensions:
      bearertokenauth:
        filename: /var/run/secrets/kubernetes.io/serviceaccount/token
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
        endpoint: tempo-tempo-gateway.$NS.svc:4317
        tls:
          ca_file: /etc/openshift-ca/service-ca.crt
          server_name_override: tempo-tempo-gateway.$NS.svc
        auth:
          authenticator: bearertokenauth
        headers:
          X-Scope-OrgID: "$TENANT"
      debug:
        verbosity: basic
    service:
      extensions: [bearertokenauth]
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
# tempo-tempo-0           3/3   Running  (tempo + tempo-gateway + tempo-gateway-opa containers)
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

Wait ~15 seconds for the batch processor to flush, then query Tempo through the gateway (which enforces HTTPS and bearer auth):

```sh
# Terminal 1 — port-forward the gateway query port
oc port-forward -n $NS svc/tempo-tempo-gateway 8080:8080

# Terminal 2 — retrieve the trace by ID (requires your OpenShift token)
TOKEN=$(oc whoami -t)
curl -sk "https://localhost:8080/api/traces/v1/$TENANT/tempo/api/traces/$TRACE_ID" \
  -H "Authorization: Bearer $TOKEN" \
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
# with port-forward to tempo-tempo-gateway on 8080 still active
TOKEN=$(oc whoami -t)
curl -sk "https://localhost:8080/api/traces/v1/$TENANT/tempo/api/search/tags" \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
# Expected tagNames to include: service.name, k8s.namespace.name, k8s.pod.name
```

## Accessing the Traces UI

The recommended way to view traces on OpenShift is through the OpenShift Console's built-in **Observe → Traces** view, powered by the Cluster Observability Operator (COO). The deprecated Jaeger UI bundled in TempoMonolithic is not used.

### Install the Cluster Observability Operator

```sh
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cluster-observability-operator
  namespace: openshift-operators
spec:
  channel: stable
  name: cluster-observability-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

Wait for it to reach `Succeeded`:

```sh
oc get csv -n openshift-operators | grep cluster-observability
```

### Enable the distributed tracing UI plugin

```sh
cat <<EOF | oc apply -f -
apiVersion: observability.openshift.io/v1alpha1
kind: UIPlugin
metadata:
  name: distributed-tracing
spec:
  type: DistributedTracing
EOF
```

### View traces

In the OpenShift Console, go to **Observe → Traces**. Select your Tempo instance (`tempo` in the namespace where you deployed it) and search by attribute:

- `service.name = openclaw` — gateway spans
- `k8s.namespace.name = <your-namespace>` — all spans from a specific namespace

Use the **TraceQL** query editor for advanced filtering, e.g.:
```
{ resource.service.name = "openclaw" && duration > 500ms }
```

The scatter plot shows trace start time vs. duration; click any trace to open the Gantt chart view with per-span attribute detail.

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

## Optional: Log Storage with LokiStack

Log forwarding is functional as-is — the collector receives OTLP logs from the gateway and the `debug` exporter confirms receipt. Persistent log storage with a queryable UI requires additional infrastructure and is **optional**.

### When to add it

- Your cluster already has the **Loki Operator** and **Red Hat OpenShift Logging Operator** installed
- You need logs searchable in **Observe → Logs** in the OpenShift Console
- You are running on a cluster with sufficient capacity (LokiStack `1x.extra-small` needs ~4–5 vCPU and ~8 GiB beyond the base workload)

### Prerequisites

OpenShift Logging 6.5+ (OTLP ingestion is GA). The Loki and Logging operators install into `openshift-operators-redhat`, which requires an OperatorGroup first:

```sh
# Ensure the OperatorGroup exists (idempotent)
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-operators-redhat
  namespace: openshift-operators-redhat
spec: {}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: loki-operator
  namespace: openshift-operators-redhat
spec:
  channel: stable-6.5
  name: loki-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cluster-logging
  namespace: openshift-operators-redhat
spec:
  channel: stable-6.5
  name: cluster-logging
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

Wait for both to reach `Succeeded`:

```sh
oc get csv -n openshift-operators-redhat | grep -E "loki|logging"
```

### Deploy LokiStack

LokiStack requires S3-compatible object storage. Provide credentials as a Secret:

```sh
# Create the secret (replace with your actual S3 credentials)
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: lokistack-s3
  namespace: $NS
stringData:
  access_key_id: <YOUR_ACCESS_KEY>
  access_key_secret: <YOUR_SECRET_KEY>
  bucketnames: <YOUR_BUCKET>
  endpoint: <S3_ENDPOINT>   # omit for AWS S3 native
  region: us-east-1
EOF

cat <<EOF | oc apply -f -
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: logging-loki
  namespace: $NS
spec:
  size: 1x.extra-small
  storageClassName: gp3-csi   # required; use your cluster's default storage class
  storage:
    schemas:
    - version: v13
      effectiveDate: "2024-01-01"
    secret:
      name: lokistack-s3
      type: s3
  tenants:
    mode: openshift-logging
  limits:
    global:
      otlp:
        streamLabels:
          resourceAttributes:
          - name: "k8s.namespace.name"
          - name: "k8s.pod.name"
          - name: "k8s.container.name"
          - name: "service.name"
  # If your cluster has infra nodes with NoSchedule taints, add tolerations
  # so LokiStack components can schedule there when worker nodes are full:
  template:
    compactor:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    distributor:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    gateway:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    indexGateway:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    ingester:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    querier:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    queryFrontend:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
    ruler:
      tolerations: [{key: "node-role.kubernetes.io/infra", operator: "Exists", effect: "NoSchedule"}]
EOF
```

### Forward logs to LokiStack via ClusterLogForwarder

Log collection into LokiStack is done by the **ClusterLogForwarder** (CLF), a DaemonSet of vector agents that collect pod logs node-locally and push them to LokiStack over OTLP. This is the right tool for log aggregation — the OTel Collector is for traces, not log collection.

First, create a ServiceAccount and grant it the required collection roles:

```sh
oc create sa collector -n $NS
oc adm policy add-cluster-role-to-user collect-application-logs -z collector -n $NS
oc adm policy add-cluster-role-to-user logging-collector-logs-writer -z collector -n $NS
```

Then deploy the ClusterLogForwarder. The `tls.ca` field is required to trust the LokiStack gateway's OpenShift service-serving certificate:

```sh
cat <<EOF | oc apply -f -
apiVersion: observability.openshift.io/v1
kind: ClusterLogForwarder
metadata:
  name: collector
  namespace: $NS
spec:
  serviceAccount:
    name: collector
  outputs:
  - name: loki-app
    type: lokiStack
    lokiStack:
      target:
        name: logging-loki
        namespace: $NS
      authentication:
        token:
          from: serviceAccount
      dataModel: Otel
    tls:
      ca:
        configMapName: openshift-service-ca.crt
        key: service-ca.crt
  pipelines:
  - name: app-logs
    inputRefs: [application]
    outputRefs: [loki-app]
EOF
```

Verify the CLF collectors are running and writing successfully:

```sh
oc get pods -n $NS -l app.kubernetes.io/component=collector
oc logs -n $NS -l app.kubernetes.io/component=collector --tail=5
# Healthy output: no WARN/error lines; vector starts quietly
```

### Enable the Logging UI Plugin

```sh
cat <<EOF | oc apply -f -
apiVersion: observability.openshift.io/v1alpha1
kind: UIPlugin
metadata:
  name: logging
spec:
  type: Logging
  logging:
    lokiStack:
      name: logging-loki
      namespace: $NS
    schema: otel
EOF
```

Logs then appear at **Observe → Logs** in the OpenShift Console. Use LogQL or the attribute filters to query by `service.name`, `k8s.namespace.name`, or any resource attribute the gateway emits.

## Troubleshooting

**Gateway receives 403 when sending OTLP to the collector**

The gateway runs behind a MITM proxy (`HTTP_PROXY=http://claw-proxy:8080`) that intercepts all outbound HTTP. If the OTel Collector endpoint is not in `NO_PROXY`, the proxy returns `403 Forbidden` because it has no credential rule for that host.

The operator automatically adds the collector hostname to `NO_PROXY` and `no_proxy` from v... (this version) onward. If you are on an older operator version or the 403 persists, verify the env var on the running gateway pod:

```sh
oc exec -n $NS <gateway-pod> -- sh -c "cat /proc/1/environ | tr '\0' '\n' | grep -i proxy"
# NO_PROXY should include the OTel Collector hostname
```

If missing, update to the latest operator image — the fix injects the OTLP endpoint hostname into `NO_PROXY` at reconcile time.

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

**Logs visible in collector but not in OpenShift Console**

The default collector logs pipeline uses only the `debug` exporter — log receipt is confirmed via `oc logs deployment/otel-collector`, but logs are not stored for UI querying. See [Optional: Log Storage with LokiStack](#optional-log-storage-with-lokistack) for the additional setup required. LokiStack is a separate infrastructure component that requires object storage and additional node capacity.
