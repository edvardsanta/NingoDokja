#!/bin/sh
set -eu

# The image stores the exact locked packages. Fail locally if manifests changed;
# rebuild the image instead of resolving dependencies during gateway startup.
pnpm install --offline --frozen-lockfile --store-dir /opt/pnpm-store

# Allows the same offline startup path to run smoke checks without logging in.
if [ "$#" -gt 0 ]; then
  exec "$@"
fi
exec pnpm dev:watch
