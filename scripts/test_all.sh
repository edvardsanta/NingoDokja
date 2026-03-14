#!/usr/bin/env bash

set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOCACHE_DIR="${GOCACHE:-/tmp/go-build}"
INCLUDE_OPENCLAW="${INCLUDE_OPENCLAW:-0}"
INCLUDE_LEGACY="${INCLUDE_LEGACY:-0}"

REPORT_ROOT="$ROOT_DIR/test_reports"
RUN_ID="$(date '+%Y%m%d_%H%M%S')"
RUN_DIR="$REPORT_ROOT/$RUN_ID"
LATEST_LINK="$REPORT_ROOT/latest"
SUMMARY_MD="$RUN_DIR/summary.md"
SUMMARY_JSON="$RUN_DIR/summary.json"

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
SUITE_COUNT=0

mkdir -p "$RUN_DIR/logs"

json_escape() {
  printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'
}

append_json_result() {
  local name="$1"
  local status="$2"
  local command="$3"
  local log_file="$4"
  local duration_ms="$5"
  local reason="$6"

  if [ ! -f "$SUMMARY_JSON" ]; then
    printf '{\n  "run_id": %s,\n  "root": %s,\n  "results": [\n' \
      "$(json_escape "$RUN_ID")" \
      "$(json_escape "$ROOT_DIR")" >"$SUMMARY_JSON"
    FIRST_JSON_ENTRY=1
  fi

  if [ "${FIRST_JSON_ENTRY:-1}" -eq 0 ]; then
    printf ',\n' >>"$SUMMARY_JSON"
  fi
  FIRST_JSON_ENTRY=0

  printf '    {\n' >>"$SUMMARY_JSON"
  printf '      "name": %s,\n' "$(json_escape "$name")" >>"$SUMMARY_JSON"
  printf '      "status": %s,\n' "$(json_escape "$status")" >>"$SUMMARY_JSON"
  printf '      "command": %s,\n' "$(json_escape "$command")" >>"$SUMMARY_JSON"
  printf '      "log_file": %s,\n' "$(json_escape "$log_file")" >>"$SUMMARY_JSON"
  printf '      "duration_ms": %s,\n' "$duration_ms" >>"$SUMMARY_JSON"
  printf '      "reason": %s\n' "$(json_escape "$reason")" >>"$SUMMARY_JSON"
  printf '    }' >>"$SUMMARY_JSON"
}

record_result() {
  local name="$1"
  local status="$2"
  local command="$3"
  local log_file="$4"
  local duration_ms="$5"
  local reason="${6:-}"

  SUITE_COUNT=$((SUITE_COUNT + 1))

  case "$status" in
    pass) PASS_COUNT=$((PASS_COUNT + 1)) ;;
    fail) FAIL_COUNT=$((FAIL_COUNT + 1)) ;;
    skip) SKIP_COUNT=$((SKIP_COUNT + 1)) ;;
  esac

  printf '[%s] %s\n' "$(printf '%s' "$status" | tr '[:lower:]' '[:upper:]')" "$name"
  if [ -n "$reason" ]; then
    printf '  reason: %s\n' "$reason"
  fi
  if [ -n "$log_file" ]; then
    printf '  log: %s\n' "$log_file"
  fi

  append_json_result "$name" "$status" "$command" "$log_file" "$duration_ms" "$reason"

  {
    printf '| %s | %s | `%s` | %sms | %s | %s |\n' \
      "$name" "$status" "$command" "$duration_ms" "$([ -n "$reason" ] && printf '%s' "$reason" || printf '-')" "$log_file"
  } >>"$SUMMARY_MD"
}

run_suite() {
  local name="$1"
  local command="$2"
  local workdir="$3"
  local precheck="$4"
  local skip_reason="$5"

  local safe_name
  safe_name="$(printf '%s' "$name" | tr '/ ' '__')"
  local log_file="$RUN_DIR/logs/${safe_name}.log"

  if ! eval "$precheck"; then
    : >"$log_file"
    record_result "$name" "skip" "$command" "$log_file" 0 "$skip_reason"
    return
  fi

  local start_ts end_ts duration_ms
  start_ts="$(date +%s)"

  if (cd "$workdir" && eval "$command") >"$log_file" 2>&1; then
    end_ts="$(date +%s)"
    duration_ms=$(( (end_ts - start_ts) * 1000 ))
    record_result "$name" "pass" "$command" "$log_file" "$duration_ms"
  else
    end_ts="$(date +%s)"
    duration_ms=$(( (end_ts - start_ts) * 1000 ))
    record_result "$name" "fail" "$command" "$log_file" "$duration_ms"
  fi
}

{
  printf '# Test Report\n\n'
  printf -- '- Run ID: `%s`\n' "$RUN_ID"
  printf -- '- Root: `%s`\n\n' "$ROOT_DIR"
  printf '| Suite | Status | Command | Duration | Reason | Log |\n'
  printf '| --- | --- | --- | ---: | --- | --- |\n'
} >"$SUMMARY_MD"

printf 'Running Dokja test suites from %s\n' "$ROOT_DIR"
printf 'Report directory: %s\n' "$RUN_DIR"

run_suite \
  "dokja_orch" \
  "GOCACHE='$GOCACHE_DIR' go test ./..." \
  "$ROOT_DIR/dokja_orch" \
  "[ -f '$ROOT_DIR/dokja_orch/go.mod' ]" \
  "missing go.mod"

run_suite \
  "dokja_interfaces/cli" \
  "GOCACHE='$GOCACHE_DIR' go test ./..." \
  "$ROOT_DIR/dokja_interfaces/cli" \
  "[ -f '$ROOT_DIR/dokja_interfaces/cli/go.mod' ]" \
  "missing go.mod"

run_suite \
  "dokja_services/dokja_scheduler" \
  "GOCACHE='$GOCACHE_DIR' go test ./..." \
  "$ROOT_DIR/dokja_services/dokja_scheduler" \
  "[ -f '$ROOT_DIR/dokja_services/dokja_scheduler/go.mod' ]" \
  "missing go.mod"

run_suite \
  "dokja_domain/dokja_meme" \
  "GOCACHE='$GOCACHE_DIR' go test ./..." \
  "$ROOT_DIR/dokja_domain/dokja_meme" \
  "[ -f '$ROOT_DIR/dokja_domain/dokja_meme/go.mod' ]" \
  "missing go.mod"

run_suite \
  "dokja_domain/dokja_moderation" \
  "GOCACHE='$GOCACHE_DIR' go test ./..." \
  "$ROOT_DIR/dokja_domain/dokja_moderation" \
  "[ -f '$ROOT_DIR/dokja_domain/dokja_moderation/go.mod' ]" \
  "missing go.mod"

if [ "$INCLUDE_LEGACY" = "1" ]; then
  run_suite \
    "dokja_legacy" \
    "GOCACHE='$GOCACHE_DIR' go test ./..." \
    "$ROOT_DIR/dokja_legacy" \
    "[ -f '$ROOT_DIR/dokja_legacy/go.mod' ]" \
    "missing go.mod"
fi

run_suite \
  "dokja_interfaces/discord" \
  "pnpm test" \
  "$ROOT_DIR/dokja_interfaces/discord" \
  "command -v pnpm >/dev/null 2>&1 && [ -f '$ROOT_DIR/dokja_interfaces/discord/package.json' ]" \
  "pnpm not installed or missing package.json"

run_suite \
  "dokja_lab" \
  "python3 -m pytest" \
  "$ROOT_DIR/dokja_lab" \
  "command -v python3 >/dev/null 2>&1 && python3 -c 'import pytest' >/dev/null 2>&1" \
  "python3/pytest not installed"

if [ "$INCLUDE_OPENCLAW" = "1" ]; then
  run_suite \
    "dokja_interfaces/openclaw" \
    "pnpm test" \
    "$ROOT_DIR/dokja_interfaces/openclaw" \
    "command -v pnpm >/dev/null 2>&1 && [ -f '$ROOT_DIR/dokja_interfaces/openclaw/package.json' ]" \
    "pnpm not installed or missing package.json"
fi

{
  printf '\n## Totals\n\n'
  printf -- '- Suites: %d\n' "$SUITE_COUNT"
  printf -- '- Passed: %d\n' "$PASS_COUNT"
  printf -- '- Failed: %d\n' "$FAIL_COUNT"
  printf -- '- Skipped: %d\n' "$SKIP_COUNT"
} >>"$SUMMARY_MD"

if [ -f "$SUMMARY_JSON" ]; then
  {
    printf '\n  ],\n'
    printf '  "totals": {\n'
    printf '    "suites": %d,\n' "$SUITE_COUNT"
    printf '    "passed": %d,\n' "$PASS_COUNT"
    printf '    "failed": %d,\n' "$FAIL_COUNT"
    printf '    "skipped": %d\n' "$SKIP_COUNT"
    printf '  }\n'
    printf '}\n'
  } >>"$SUMMARY_JSON"
fi

rm -f "$LATEST_LINK"
ln -s "$RUN_DIR" "$LATEST_LINK"

printf '\nSummary: %d suites passed, %d failed, %d skipped\n' "$PASS_COUNT" "$FAIL_COUNT" "$SKIP_COUNT"
printf 'Markdown report: %s\n' "$SUMMARY_MD"
printf 'JSON report: %s\n' "$SUMMARY_JSON"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
