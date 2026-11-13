#!/usr/bin/env bash
# Tests of voice-host.sh with a fake interpreter: no model, no download, no network.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM
fail() { echo "FAIL: $*" >&2; exit 1; }

mkdir -p "$work/venv/bin"
cat > "$work/venv/bin/python" <<'PY'
#!/usr/bin/env bash
env | grep -E '^(DOKJA_VOICE_|HSA_|SECRET_)' | sort
echo "args=$*"
PY
chmod +x "$work/venv/bin/python"
cat > "$work/env" <<'ENV'
DOKJA_VOICE_PIPER_MODEL=/workspace/.dokja/models/voice/model.onnx
DOKJA_VOICE_STT_MODEL="openai/whisper-tiny"
DISCORD_BOT_TOKEN=must-not-leak
SECRET_VALUE=must-not-leak
ENV

run() { env -u DOKJA_VOICE_ACOUSTIC_DEVICE -u HSA_OVERRIDE_GFX_VERSION ENV_FILE="$work/env" VOICE_VENV="$work/venv" "$@" "$repo_root/scripts/voice-host.sh" 2>/dev/null; }

out="$(run)"
grep -qx "DOKJA_VOICE_PIPER_MODEL=$repo_root/.dokja/models/voice/model.onnx" <<<"$out" || fail "a /workspace path is not mapped to the checkout"
grep -qx "DOKJA_VOICE_STT_MODEL=openai/whisper-tiny" <<<"$out" || fail "quotes are not stripped"
grep -qx "DOKJA_VOICE_HOST=0.0.0.0" <<<"$out" || fail "the default bind is not 0.0.0.0"
grep -qx "DOKJA_VOICE_ACOUSTIC_DEVICE=rocm" <<<"$out" || fail "the classifier does not default to rocm"
grep -qx "HSA_OVERRIDE_GFX_VERSION=10.3.0" <<<"$out" || fail "the GFX override is missing"
grep -q "^args=main.py" <<<"$out" || fail "main.py is not started"
! grep -q "must-not-leak" <<<"$out" || fail "a key outside DOKJA_VOICE_* reached the service"

out="$(run env VOICE_BIND=127.0.0.1 DOKJA_VOICE_ACOUSTIC_DEVICE=cpu)"
grep -qx "DOKJA_VOICE_HOST=127.0.0.1" <<<"$out" || fail "VOICE_BIND is ignored"
grep -qx "DOKJA_VOICE_ACOUSTIC_DEVICE=cpu" <<<"$out" || fail "an explicit device is overridden"

if ENV_FILE="$work/env" VOICE_VENV="$work/missing" "$repo_root/scripts/voice-host.sh" >/dev/null 2>"$work/err"; then fail "a missing virtualenv is not an error"; fi
grep -q "voice-host.sh setup" "$work/err" || fail "the missing-virtualenv message does not say how to fix it"
echo "ok"
