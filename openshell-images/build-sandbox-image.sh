#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

image="${1:-openclaw-openshell-sandbox:local}"

build_args=()
if [[ "${INSTALL_BUILD_TOOLS:-false}" == "true" ]]; then
  build_args+=(--build-arg INSTALL_BUILD_TOOLS=true)
fi

podman build \
  -t "$image" \
  -f "$script_dir/Dockerfile.sandbox" \
  "${build_args[@]}" \
  "$script_dir"

podman run --rm "$image" sh -lc \
  'test "$(id -u)" = "65532" && test "$(id -g)" = "65532" && command -v bash && command -v git && command -v node && command -v npm && command -v python3 && ! command -v ssh && ! command -v scp && ! command -v sftp && ! command -v ssh-keygen && ! command -v ssh-agent && ! command -v ssh-add && ! command -v wget && ! command -v rsync'
