#!/usr/bin/env bash
set -euo pipefail
ROOT="$(dirname "$(dirname "$(realpath "$0")")")"
cd "$ROOT"
if [[ -f .env ]]; then
  set -a
  source .env
  set +a
fi
export PATH="$ROOT/.venv/bin:$ROOT/.tools:$ROOT/.tools/go/bin:$PATH"
export PYTHON_BIN="${PYTHON_BIN:-$ROOT/.venv/bin/python}"
export YTDLP_BIN="${YTDLP_BIN:-$ROOT/.venv/bin/yt-dlp}"
export SHORTS_SCRIPT="$ROOT/scripts/shorts.py"
export HF_HOME="${HF_HOME:-$ROOT/data/models}"
export TTS_DOCKER_AUTO_MANAGE="${TTS_DOCKER_AUTO_MANAGE:-false}"
if [[ ! -x "$PYTHON_BIN" ]]; then
  echo 'Install dependencies first: python3 -m venv .venv && .venv/bin/pip install -r requirements-shorts.txt' >&2
  exit 1
fi
mkdir -p bin data
if command -v go >/dev/null; then
  go build -o bin/api ./cmd/api
elif [[ ! -x bin/api ]]; then
  echo 'Go 1.25+ is required to build the server (or use Docker Compose).' >&2
  exit 1
fi
exec ./bin/api
