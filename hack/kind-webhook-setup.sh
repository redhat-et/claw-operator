#!/usr/bin/env bash
# kind-webhook-setup.sh — generate self-signed webhook TLS certs and deploy them
# to a Kind (or plain Kubernetes) cluster so the admission webhooks work without
# the OpenShift service CA operator.
#
# Usage:
#   ./hack/kind-webhook-setup.sh [NAMESPACE]
#
# NAMESPACE defaults to claw-operator (set by kustomize namePrefix + namespace).
set -euo pipefail

NS="${1:-claw-operator}"
WEBHOOK_SVC="claw-operator-webhook-service"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Generating self-signed CA and server certificate for webhook..."

# CA key + certificate
openssl genrsa -out "$TMPDIR/ca.key" 2048 2>/dev/null
openssl req -x509 -new -nodes -key "$TMPDIR/ca.key" \
  -subj "/CN=claw-webhook-ca" \
  -days 3650 -out "$TMPDIR/ca.crt" 2>/dev/null

# Server key + CSR
openssl genrsa -out "$TMPDIR/server.key" 2048 2>/dev/null
cat > "$TMPDIR/san.cnf" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${WEBHOOK_SVC}
DNS.2 = ${WEBHOOK_SVC}.${NS}
DNS.3 = ${WEBHOOK_SVC}.${NS}.svc
DNS.4 = ${WEBHOOK_SVC}.${NS}.svc.cluster.local
EOF

openssl req -new -key "$TMPDIR/server.key" \
  -subj "/CN=${WEBHOOK_SVC}.${NS}.svc" \
  -out "$TMPDIR/server.csr" \
  -config "$TMPDIR/san.cnf" 2>/dev/null

openssl x509 -req -in "$TMPDIR/server.csr" \
  -CA "$TMPDIR/ca.crt" -CAkey "$TMPDIR/ca.key" -CAcreateserial \
  -out "$TMPDIR/server.crt" \
  -days 3650 \
  -extensions v3_req \
  -extfile "$TMPDIR/san.cnf" 2>/dev/null

echo "==> Creating namespace ${NS} (if not present)..."
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -

echo "==> Creating/updating webhook-server-cert Secret in ${NS}..."
kubectl create secret tls webhook-server-cert \
  --cert="$TMPDIR/server.crt" \
  --key="$TMPDIR/server.key" \
  --namespace="$NS" \
  --dry-run=client -o yaml | kubectl apply -f -

CA_BUNDLE=$(base64 < "$TMPDIR/ca.crt" | tr -d '\n')
echo "==> Patching MutatingWebhookConfiguration with CA bundle..."
kubectl patch mutatingwebhookconfiguration claw-operator-mutating-webhook-configuration \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"/webhooks/0/clientConfig/caBundle\",\"value\":\"${CA_BUNDLE}\"}]" 2>/dev/null || true

echo "==> Patching ValidatingWebhookConfiguration with CA bundle..."
kubectl patch validatingwebhookconfiguration claw-operator-validating-webhook-configuration \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"/webhooks/0/clientConfig/caBundle\",\"value\":\"${CA_BUNDLE}\"}]" 2>/dev/null || true

echo "==> Done. Webhook TLS is ready."
echo "    CA bundle stored in namespace ${NS} Secret webhook-server-cert."
