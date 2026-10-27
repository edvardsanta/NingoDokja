#!/usr/bin/env bash
# Build the mounted orchestrator offline using only the candidate image's module cache.
# Usage: bash scripts/tests/test_orchestrator_module_cache.sh IMAGE [podman|docker]
set -euo pipefail

image="${1:?Pass the candidate orchestrator development image}"
engine="${2:-podman}"
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"

"$engine" run --rm --pull=never --network none \
  --mount "type=bind,source=$repo_root,target=/workspace,readonly" \
  --env GOPROXY=off --env GOSUMDB=off --env GOTOOLCHAIN=local \
  --env GOCACHE=/tmp/offline-go-build \
  "$image" go build -mod=readonly -o /tmp/dokja-orch-offline-check ./cmd/va
