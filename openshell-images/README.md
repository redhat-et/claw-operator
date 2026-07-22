# OpenShell Images

This directory contains the image definitions used by the claw-operator
OpenShell direct-endpoint integration.

## Images

- [Dockerfile.openclaw](Dockerfile.openclaw) layers the OpenShell CLI and the
  minimal local `ssh` runtime needed by the OpenClaw OpenShell sandbox plugin
  onto an OpenClaw base image. The final image does not install the full
  OpenSSH client package, `scp`, `sftp`, SSH key/agent helpers, `rsync`, or
  build tools.
- [Dockerfile.sandbox](Dockerfile.sandbox) builds the UBI Minimal-based
  OpenShell sandbox image used for agent exec sessions. It keeps common shell,
  archive, Git, Python, and Node tooling, defaults to a non-root `sandbox`
  user, strips setuid/setgid bits, and avoids replacing UBI's
  `coreutils-single` and `curl-minimal` packages with conflicting larger
  packages.

## Build OpenClaw With OpenShell CLI

From the `claw-operator` repo root:

```shell
./openshell-images/build-openclaw-source-image.sh quay.io/<org>/openclaw:openshell
podman push quay.io/<org>/openclaw:openshell
```

The helper builds `../openclaw` from source with the core OpenClaw extensions,
then layers [Dockerfile.openclaw](Dockerfile.openclaw) on top. Override
`OPENCLAW_DIR`, `OPENCLAW_BASE_IMAGE`, or `OPENSHELL_CLI_VERSION` when needed.

To layer the CLI onto an existing OpenClaw image directly:

```shell
podman build -t quay.io/<org>/openclaw:openshell \
  -f openshell-images/Dockerfile.openclaw \
  --build-arg OPENCLAW_BASE_IMAGE=ghcr.io/openclaw/openclaw:latest \
  openshell-images
```

## Build Sandbox Image

From the `claw-operator` repo root:

```shell
./openshell-images/build-sandbox-image.sh quay.io/<org>/openclaw-openshell-sandbox:latest
podman push quay.io/<org>/openclaw-openshell-sandbox:latest
```

Optional build tools remain opt-in:

```shell
INSTALL_BUILD_TOOLS=true \
  ./openshell-images/build-sandbox-image.sh quay.io/<org>/openclaw-openshell-sandbox:build-tools
```

Use the pushed sandbox image in `Claw.spec.openshell.sandboxImage`.

These images do not deploy or configure an OpenShell gateway. Deploy the
gateway separately, then point `Claw.spec.openshell.gatewayEndpoint` at its
in-cluster Service DNS name.
