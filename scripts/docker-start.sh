#!/usr/bin/env bash
set -euo pipefail
ROOT="$(dirname "$(dirname "$(realpath "$0")")")"
cd "$ROOT"
# Wait before building on an already-hot laptop (sysfs values are millidegrees).
for zone in /sys/class/thermal/thermal_zone*; do
  [[ -r "$zone/type" && -r "$zone/temp" ]] || continue
  read -r kind < "$zone/type"
  [[ "$kind" == x86_pkg_temp || "$kind" == cpu-thermal ]] || continue
  read -r temp < "$zone/temp"
  if (( temp >= 80000 )); then
    echo "CPU is hot; waiting for temperature below 70°C before building."
    while (( temp > 70000 )); do
      sleep 5
      read -r temp < "$zone/temp"
    done
  fi
done
compose=(docker compose -f docker-compose.yml)
if [[ -e /dev/dri/renderD128 ]]; then
  compose+=(-f docker-compose.intel.yml)
fi
if [[ -e /dev/accel/accel0 ]]; then
  compose+=(-f docker-compose.npu.yml)
fi
if [[ "${1:-}" == "--studio" ]]; then
  compose+=(--profile tts)
fi
# A dedicated BuildKit container bounds build work too; service limits apply only
# after startup. Reusing the builder retains dependency caches for later builds.
builder=aicf-low-resource
if ! docker buildx inspect "$builder" >/dev/null 2>&1; then
  docker buildx create --name "$builder" --driver docker-container \
    --driver-opt cpu-quota=200000,cpu-period=100000,memory=3g,memory-swap=3g >/dev/null
fi
export BUILDX_BUILDER="$builder" COMPOSE_PARALLEL_LIMIT=1
trap 'docker buildx stop "$builder" >/dev/null 2>&1 || true' EXIT
"${compose[@]}" build api
if [[ "${1:-}" == "--studio" ]]; then
  "${compose[@]}" build tts
fi
"${compose[@]}" up -d --no-build
"${compose[@]}" ps
