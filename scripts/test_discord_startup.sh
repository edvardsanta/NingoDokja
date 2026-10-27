#!/usr/bin/env bash
# Exercise a fresh dev dependency installation with networking completely disabled.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${1:?Usage: scripts/test_discord_startup.sh <dev-image>}"
engine="${CONTAINER_ENGINE:-docker}"
"$engine" run --rm --network none \
  -v "$repo_root/dokja_services/dokja_discord:/fixture:ro" \
  --entrypoint sh "$image" -eu -c '
    mkdir -p /tmp/discord-startup
    tar -C /fixture --exclude=node_modules -cf - . | tar -C /tmp/discord-startup -xf -
    cd /tmp/discord-startup
    cp pnpm-lock.yaml /tmp/expected-lock.yaml
    cat > offline_probe.ts <<"PROBE"
import assert from "node:assert/strict";
import { createAudioPlayer } from "@discordjs/voice";
const n: number = 1;
assert.equal(n, 1);
assert.ok(createAudioPlayer());
console.log("Offline TypeScript and voice dependencies loaded.");
PROBE
    /usr/local/bin/dokja-discord-dev node --import tsx ./offline_probe.ts
    cmp pnpm-lock.yaml /tmp/expected-lock.yaml
    node --input-type=module -e '\''
      import fs from "node:fs";
      const p = JSON.parse(fs.readFileSync("package.json", "utf8"));
      p.dependencies["missing-fixture-package"] = "0.0.0";
      fs.writeFileSync("package.json", JSON.stringify(p));
    '\''
    if /usr/local/bin/dokja-discord-dev sh -c "touch /tmp/unexpected-start" > /tmp/expected-failure.log 2>&1; then
      echo "Changed manifests unexpectedly started" >&2
      exit 1
    fi
    test ! -e /tmp/unexpected-start
    echo "Changed manifests refused before gateway startup."
  '
