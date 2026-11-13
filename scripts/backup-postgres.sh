#!/usr/bin/env bash
# Dump the stack's PostgreSQL (price history, macro context, anything in the dokja database) to a
# private, timestamped file, check that it can be read back, and keep only the newest few.
#
#   make backup                     uses the same compose files as `make up`
#   scripts/backup-postgres.sh      uses `docker compose` and the default stack in the current directory
#
# The dump is pg_dump's custom format, so one schema can be restored on its own. pg_restore -n
# restores what is inside the schema, not the schema itself, so create it first:
#   psql -d <database> -c 'CREATE SCHEMA dokja_price'
#   pg_restore --no-owner -n dokja_price -d <database> dokja-YYYYmmdd-HHMMSS.dump
#
# Settings (environment):
#   COMPOSE_CMD   the compose command, with its files (default: docker compose)
#   PG_EXEC       a command that runs another command in the database container, reading stdin
#                 (default: "$COMPOSE_CMD exec -T $SERVICE"); the tests point it at a disposable container
#   SERVICE       the compose service (default: dokja-postgres)
#   PGUSER, PGDATABASE   (default: dokja, dokja)
#   BACKUP_DIR    where dumps go (default: .dokja/backups/postgres, which git ignores)
#   KEEP          how many dumps to keep (default: 14)
set -euo pipefail

COMPOSE_CMD="${COMPOSE_CMD:-docker compose}"
SERVICE="${SERVICE:-dokja-postgres}"
PGUSER="${PGUSER:-dokja}"
PGDATABASE="${PGDATABASE:-dokja}"
BACKUP_DIR="${BACKUP_DIR:-.dokja/backups/postgres}"
KEEP="${KEEP:-14}"
PG_EXEC="${PG_EXEC:-$COMPOSE_CMD exec -T $SERVICE}"

case "$KEEP" in '' | *[!0-9]* | 0) echo "KEEP must be a positive number" >&2; exit 2 ;; esac

umask 077
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
stamp="$(date +%Y%m%d-%H%M%S)"
final="$BACKUP_DIR/dokja-$stamp.dump"
partial="$final.partial"
trap 'rm -f "$partial"' EXIT

# A dump is written beside its final name and moved into place only once it is whole.
if ! $PG_EXEC pg_dump -U "$PGUSER" -d "$PGDATABASE" --format=custom --no-owner --no-privileges > "$partial"; then
  echo "pg_dump failed; nothing was kept" >&2
  exit 1
fi
if [ ! -s "$partial" ]; then
  echo "the dump is empty; nothing was kept" >&2
  exit 1
fi
# pg_restore --list reads the whole table of contents: a truncated or corrupt dump fails here.
if ! listing="$($PG_EXEC pg_restore --list < "$partial")"; then
  echo "the dump cannot be read back; nothing was kept" >&2
  exit 1
fi
mv "$partial" "$final"
trap - EXIT

size="$(du -h "$final" | cut -f1)"
tables="$(printf '%s\n' "$listing" | grep -c ' TABLE DATA ' || true)"
schemas="$(printf '%s\n' "$listing" | awk '$0 ~ / SCHEMA - / { print $(NF-1) }' | sort -u | tr '\n' ' ')"
echo "backup: $final ($size, $tables tables with data; schemas: ${schemas:-none})"

# Retention only touches this tool's own files.
count=0
while IFS= read -r old; do
  count=$((count + 1))
  if [ "$count" -gt "$KEEP" ]; then
    rm -f -- "$old"
    echo "removed old backup: $old"
  fi
done < <(ls -1t "$BACKUP_DIR"/dokja-*.dump 2>/dev/null || true)
