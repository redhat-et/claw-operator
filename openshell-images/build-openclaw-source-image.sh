#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

image="${1:-openclaw-openshell:local}"
openclaw_dir="${OPENCLAW_DIR:-${repo_root}/../openclaw}"
base_image="${OPENCLAW_BASE_IMAGE:-${image}-base}"

podman build \
  -t "$base_image" \
  -f "$openclaw_dir/Dockerfile" \
  --build-arg OPENCLAW_EXTENSIONS=diagnostics-otel,codex \
  "$openclaw_dir"

podman build \
  -t "$image" \
  -f "$script_dir/Dockerfile.openclaw" \
  --build-arg OPENCLAW_BASE_IMAGE="$base_image" \
  --build-arg OPENSHELL_CLI_VERSION="${OPENSHELL_CLI_VERSION:-0.0.44}" \
  "$script_dir"

podman run --rm "$image" sh -lc \
  'test -x /opt/openshell/bin/openshell && /opt/openshell/bin/openshell --version && command -v ssh && ! command -v scp && ! command -v sftp && ! command -v ssh-keygen && ! command -v ssh-agent && ! command -v ssh-add && ! command -v rsync && test -f /app/dist/extensions/diagnostics-otel/package.json && test ! -e /app/dist/extensions/openshell/package.json'
