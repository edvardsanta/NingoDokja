#!/usr/bin/env bash
# Run dokja-voice on the host, outside a container, with a ROCm build of torch.
#
#   scripts/voice-host.sh setup   create the virtualenv and install the requirements (large download)
#   scripts/voice-host.sh         run the service in the foreground
#
# Why: the ROCm image does not fit the container storage of a small machine, and a host process
# reaches the GPU directly. The containers (orchestrator, Discord) call it over HTTP, so point
# DOKJA_VOICE_HTTP_ENDPOINT at host.containers.internal, port 8081, in the env file.
#
# Only the DOKJA_VOICE_* keys of the env file are read, never the rest of it: it holds tokens.
# A path under /workspace (the container mount) is mapped to this checkout.
#
# Settings (environment):
#   ENV_FILE        the env file to read (default: .env)
#   VOICE_VENV      the virtualenv (default: .dokja/voice-venv, which git ignores)
#   VOICE_BIND      the address to listen on (default: 0.0.0.0). Containers cannot reach a host
#                   loopback address, so the port must be closed to your network by a firewall.
#   VOICE_PYTHON    the interpreter for setup (default: 3.13; the torch ROCm wheels stop at 3.13)
#   VOICE_ROCM      the ROCm wheel index suffix for setup (default: rocm6.3)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${ENV_FILE:-.env}"
venv="${VOICE_VENV:-$root/.dokja/voice-venv}"
service="$root/dokja_services/dokja_voice"

setup() {
  command -v uv >/dev/null || { echo "uv is required (the astral uv installer)" >&2; exit 1; }
  local rocm="${VOICE_ROCM:-rocm6.3}" base index
  # The wheel index is the one requirements.txt already names for CPU torch, with the ROCm suffix.
  index="$(sed -n "s|^--extra-index-url \(.*\)/cpu\$|\1/$rocm|p" "$service/requirements.txt")"
  [ -n "$index" ] || { echo "No --extra-index-url ending in /cpu in $service/requirements.txt" >&2; exit 1; }
  base="$(mktemp)"
  trap 'rm -f "$base"' RETURN
  sed '/^--extra-index-url /d; /^torch==/d' "$service/requirements.txt" > "$base"
  uv venv --python "${VOICE_PYTHON:-3.13}" "$venv"
  uv pip install --python "$venv/bin/python" -r "$base" fastapi uvicorn -r "$service/requirements-vad.txt"
  uv pip install --python "$venv/bin/python" --index-url "$index" torch==2.8.0
  echo "Done. uv keeps the downloaded wheels in its cache; 'uv cache clean' gives that space back."
}

load_env() {
  [ -f "$env_file" ] || return 0
  local line key value
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^(DOKJA_VOICE_[A-Z0-9_]+)=(.*)$ ]] || continue
    key="${BASH_REMATCH[1]}"
    value="${BASH_REMATCH[2]}"
    value="${value%\"}"; value="${value#\"}"; value="${value%\'}"; value="${value#\'}"
    [ -z "${!key+x}" ] || continue
    export "$key=${value//\/workspace/$root}"
  done < "$env_file"
}

if [ "${1:-}" = "setup" ]; then
  setup
  exit 0
fi

if [ ! -x "$venv/bin/python" ]; then
  echo "No virtualenv at $venv. Run: scripts/voice-host.sh setup" >&2
  exit 1
fi

load_env
export DOKJA_VOICE_HOST="${VOICE_BIND:-0.0.0.0}"
export DOKJA_VOICE_PORT="${DOKJA_VOICE_PORT:-8081}"
export DOKJA_VOICE_ACOUSTIC_DEVICE="${DOKJA_VOICE_ACOUSTIC_DEVICE:-rocm}"
ollama_port="${DOKJA_OLLAMA_HOST_PORT:-11434}"
ollama_url="http://127.0.0.1:${ollama_port}"
export DOKJA_VOICE_CHAT_OLLAMA_URL="${DOKJA_VOICE_CHAT_OLLAMA_URL:-$ollama_url}"
# The same override the GPU compose overlay sets for the RX 6000 series.
export HSA_OVERRIDE_GFX_VERSION="${HSA_OVERRIDE_GFX_VERSION:-10.3.0}"

echo "dokja-voice on the host: ${DOKJA_VOICE_HOST}:${DOKJA_VOICE_PORT} (keep this port closed to your network)" >&2
cd "$service"
exec "$venv/bin/python" main.py
