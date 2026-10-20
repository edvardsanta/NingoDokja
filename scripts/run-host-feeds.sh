#!/usr/bin/env bash
# Run the reading host (Feeds) on this machine, for docker-compose.host-feeds.yml.
#
#   scripts/run-host-feeds.sh
#
# It listens on a unix socket in DOKJA_FEEDS_SOCKET_DIR (default $XDG_RUNTIME_DIR/dokja-feeds),
# a directory only this user can open, and reads plugins from FEEDS_PLUGINS_DIR (default
# FEEDS_PLUGINS_HOST_DIR, then ./feeds_plugins). Plugins get only the variables their manifest
# lists, from this shell: export AGENT_KINDS_* here to configure the agent-kinds classifier.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
socket_dir="${DOKJA_FEEDS_SOCKET_DIR:-${XDG_RUNTIME_DIR:?set DOKJA_FEEDS_SOCKET_DIR or XDG_RUNTIME_DIR}/dokja-feeds}"
mkdir -p "$socket_dir"
chmod 700 "$socket_dir"

export FEEDS_SERVICE_ENDPOINT="ipc://$socket_dir/feeds.sock"
export FEEDS_PLUGINS_DIR="${FEEDS_PLUGINS_DIR:-${FEEDS_PLUGINS_HOST_DIR:-$root/feeds_plugins}}"
export KNOWLEDGE_EXTRACTORS_DIR="${KNOWLEDGE_EXTRACTORS_DIR:-$root/dokja_services/dokja_knowledge}"

echo "Feeds on $FEEDS_SERVICE_ENDPOINT with plugins from $FEEDS_PLUGINS_DIR" >&2
echo "Start the stack with DOKJA_FEEDS_SOCKET_DIR=$socket_dir and -f docker-compose.host-feeds.yml" >&2
cd "$root/dokja_services/dokja_feeds"
exec go run ./cmd/dokja-feeds
