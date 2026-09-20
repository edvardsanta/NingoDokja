#!/usr/bin/env bash
# Scan for credentials and for things a public repository must not carry, with gitleaks.
#
#   scripts/scan-secrets.sh              commits not yet on origin/main (origin/main..HEAD)
#   scripts/scan-secrets.sh <git-range>  for example a1b2c3..HEAD, or HEAD~3..HEAD
#   scripts/scan-secrets.sh --staged     the changes staged for the next commit
#   scripts/scan-secrets.sh --all        the whole history
#
# It runs both configurations: .gitleaks.toml (default credential rules) and
# .gitleaks-policy.toml (the project's own rules). See dokja_docs/secret-scanning.md.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

if ! command -v gitleaks >/dev/null 2>&1; then
  echo "gitleaks is not installed. See dokja_docs/secret-scanning.md for how to install it." >&2
  exit 127
fi

target="${1:-origin/main..HEAD}"
status=0
for config in .gitleaks.toml .gitleaks-policy.toml; do
  echo "== gitleaks --config $config ($target)"
  case "$target" in
    --staged) scope=(--staged) ;;
    --all) scope=() ;;
    *) scope=(--log-opts="$target") ;;
  esac
  gitleaks git --config "$config" "${scope[@]}" --redact --no-banner --verbose || status=1
done
exit "$status"
