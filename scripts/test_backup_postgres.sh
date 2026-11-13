#!/usr/bin/env bash
# Synthetic tests of backup-postgres.sh against an isolated, disposable PostgreSQL container.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
container="dokja-backup-test-$$"
work="$(mktemp -d)"
image="${DOKJA_BACKUP_TEST_IMAGE:-docker.io/pgvector/pgvector:pg17}"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; rm -rf "$work"; }
trap cleanup EXIT INT TERM
fail() { echo "FAIL: $*" >&2; exit 1; }

docker run --detach --rm --name "$container" -e POSTGRES_HOST_AUTH_METHOD=trust \
  -e POSTGRES_USER=dokja -e POSTGRES_DB=dokja "$image" >/dev/null
for attempt in $(seq 1 60); do
  docker exec "$container" pg_isready -U dokja -d dokja >/dev/null 2>&1 && break
  [ "$attempt" = 60 ] && fail "the disposable database did not become ready"
  sleep 1
done
sleep 2 # the entrypoint restarts the server once after initialising
psql() { docker exec -i "$container" psql -U dokja -v ON_ERROR_STOP=1 -q -At "$@"; }

psql -d dokja <<'SQL'
CREATE SCHEMA dokja_price;
CREATE TABLE dokja_price.points (product text, cents bigint);
INSERT INTO dokja_price.points SELECT 'p' || g, g FROM generate_series(1, 250) g;
CREATE SCHEMA dokja_knowledge;
CREATE TABLE dokja_knowledge.discussion_ideas (idea text);
INSERT INTO dokja_knowledge.discussion_ideas VALUES ('one'), ('two');
SQL

export PG_EXEC="docker exec -i $container" BACKUP_DIR="$work/dumps" KEEP=2
script="$repo_root/scripts/backup-postgres.sh"

# 1. A dump is made, private, and says what it holds.
out="$("$script")"
echo "$out" | grep -q "2 tables with data" || fail "the summary does not count the tables: $out"
echo "$out" | grep -q "dokja_price" || fail "the summary does not name the schemas: $out"
dump="$(ls "$work"/dumps/dokja-*.dump)"
[ "$(stat -c %a "$work/dumps")" = 700 ] || fail "the directory is not private"
[ "$(stat -c %a "$dump")" = 600 ] || fail "the dump is not private"
[ -z "$(ls "$work"/dumps/*.partial 2>/dev/null)" ] || fail "a partial file was left behind"

# 2. One schema restores on its own, with all its rows, into an empty database.
psql -d postgres -c "CREATE DATABASE restored"
psql -d restored -c "CREATE SCHEMA dokja_price" # pg_restore -n restores the contents, not the schema itself
docker exec -i "$container" pg_restore --no-owner -n dokja_price -U dokja -d restored < "$dump"
[ "$(psql -d restored -c 'SELECT count(*) FROM dokja_price.points')" = 250 ] || fail "the restored schema lost rows"
[ "$(psql -d restored -c "SELECT count(*) FROM information_schema.schemata WHERE schema_name = 'dokja_knowledge'")" = 0 ] || fail "restoring one schema brought another"

# 3. Retention keeps the newest KEEP dumps and only dumps.
touch "$work/dumps/notes.txt"
sleep 1; "$script" >/dev/null; sleep 1; "$script" >/dev/null
[ "$(ls "$work"/dumps/dokja-*.dump | wc -l)" = 2 ] || fail "retention kept the wrong number of dumps"
[ -f "$work/dumps/notes.txt" ] || fail "retention removed a file that is not a dump"

# 4. A failing database keeps nothing and says so.
before="$(ls "$work"/dumps | sort)"
if PG_EXEC="docker exec -i no-such-container" "$script" >/dev/null 2>"$work/err"; then fail "a failing dump reported success"; fi
grep -q "nothing was kept" "$work/err" || fail "the failure was not explained"
[ "$before" = "$(ls "$work"/dumps | sort)" ] || fail "a failing dump changed what is kept"

# 5. A bad KEEP is refused before anything runs.
if KEEP=0 "$script" >/dev/null 2>&1; then fail "KEEP=0 was accepted"; fi

echo "backup-postgres tests passed"
