#!/usr/bin/env bash
# Synthetic journal tests against an isolated, disposable PostgreSQL container.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
container="dokja-journal-test-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
docker run --detach --rm --name "$container" \
  -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=journal_test \
  -p 127.0.0.1::5432 docker.io/library/postgres:17 >/dev/null
wait_ready() {
ready=false
for attempt in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d journal_test >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [ "$ready" != true ]; then
  echo "Disposable journal database did not become ready" >&2
  exit 1
fi
}
wait_ready
address="$(docker port "$container" 5432/tcp)"
port="${address##*:}"
export DOKJA_JOURNAL_TEST_DSN="host=127.0.0.1 port=$port user=postgres dbname=journal_test sslmode=disable"
cd "$repo_root/dokja_orch"
DOKJA_JOURNAL_TEST_PHASE=write go test -race -tags integration ./internal/infrastructure/application_journal -count=1 -v
# Keep the same disposable data volume, restart the server, then use a fresh Go process.
docker restart "$container" >/dev/null
wait_ready
address="$(docker port "$container" 5432/tcp)"
port="${address##*:}"
export DOKJA_JOURNAL_TEST_DSN="host=127.0.0.1 port=$port user=postgres dbname=journal_test sslmode=disable"
DOKJA_JOURNAL_TEST_PHASE=read go test -race -tags integration ./internal/infrastructure/application_journal -run '^TestPostgresServerRestartReplay$' -count=1 -v
