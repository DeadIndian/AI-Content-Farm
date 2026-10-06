# AI-Content-Farm

Go web app with a local **YouTube podcast → Shorts** workflow and the original narrated-video studio.

## Recommended: resource-limited Docker

```bash
bash scripts/docker-start.sh
```

Open **http://localhost:8080**. This builds and starts the app, automatically enabling Intel GPU access when `/dev/dri/renderD128` exists and NPU transcription when `/dev/accel/accel0` exists. Docker Engine, Compose v2.24+ and Buildx are the host software dependencies; acceleration also needs working host kernel drivers/firmware. `.env` is optional; see `.env.example` for overrides. To include the narrated Studio's local speech service, use `bash scripts/docker-start.sh --studio`.

### Resource budget

| Component | CPU ceiling | RAM ceiling | Idle behavior |
| --- | --- | --- | --- |
| API + downloader + transcription + renderer, combined | 2 CPU equivalents | 5 GiB, no swap | Speech model unloaded after jobs |
| Optional Piper Studio service | 0.75 CPU | 768 MiB, no swap | One worker, synthesis on demand |
| Dedicated image builder | 2 CPU equivalents | 3 GiB, no swap | Stopped automatically after build |

- Studio and Shorts share one heavy-work slot; only one job runs heavy processing at a time.
- Whisper uses OpenVINO on Intel NPU when the NPU overlay is enabled, otherwise CPU int8 with two inference threads. Both process 60-second audio chunks with context overlap and durable chunk checkpoints. Memory used for decoded audio does not grow with podcast length. Transcript metadata and outputs still grow with length.
- 1080×1920, 30 fps, original audio and highlighted captions are preserved. Intel VAAPI handles encoding; systems without it use a two-thread CPU fallback.
- Before transcription chunks and clips, a readable Linux CPU sensor triggers a pause at **80°C**, resuming below **70°C**. There is a two-second rest between clips. These are boundary checks, not a hard temperature cap; a single chunk/clip can warm the CPU before the next check. If sensors are unavailable, jobs report this and CPU/RAM limits remain active.
- The launcher waits for an already-hot CPU before building. Limits cannot control heat caused by other applications, the room, or the cooling system. Lower `AICF_CPUS` (e.g. `1.0`) for a slower, lighter workload.
- Downloads are limited to 5 MiB/s and one fragment at a time, preferring ≤1080p/30 fps. Completed YouTube jobs remove their downloaded originals; uploaded library videos and finished Shorts are retained. Set `SHORTS_KEEP_SOURCE=true` to retain downloads.
- Processing checks for 5 GiB free disk before chunks/clips. This is a reserve check, not a disk quota; downloads and uploads can still consume disk. Logs rotate at 2×5 MB per service.
- Autopilot is disabled in Docker. No background content generation occurs until you submit a job.

`base` is the low-resource default model. `WHISPER_MODEL=small` improves recognition at the cost of more time/memory; captions should be reviewed. The pipeline covers the full source in order, with fixed fit/crop/split layouts, rather than scoring viral highlights or tracking speakers.

```bash
docker compose ps
docker stats --no-stream aicf-api
docker compose logs --tail=50 api
docker compose --profile tts stop  # releases runtime CPU/RAM; keeps files
```

Data persists in `data/` and `videos/`; Piper models use a named volume. For subsequent starts without rebuilding on this Intel laptop: `docker compose -f docker-compose.yml -f docker-compose.intel.yml -f docker-compose.npu.yml up -d --no-build`. Use the launcher for bounded builds; plain `docker compose build` does not apply service CPU/RAM limits to BuildKit.

### Intel NPU transcription

`docker-compose.npu.yml` selects the NPU image target, passes only `/dev/accel/accel0`, and sets `SHORTS_TRANSCRIBER=auto`. Intel user-space driver/compiler 1.38.0 and OpenVINO GenAI 2026.3.1 are installed inside that image. The pinned driver archive is checksum-verified; host drivers are not installed or modified. The verified model is `OpenVINO/whisper-base-fp16-ov` at a pinned revision. Downloads and compiled NPU cache persist under `data/models/`.

The NPU performs Whisper inference with real word timestamps. Audio decoding, voice detection, tokenization/alignment and orchestration still use some CPU; FFmpeg encoding uses the GPU. `auto` falls back to CPU with a job warning if NPU initialization/inference fails. `SHORTS_TRANSCRIBER=npu` requires NPU success; `cpu` explicitly selects faster-whisper. Other Whisper model sizes currently use CPU fallback. The 5 GiB limit is for the main container; optional Piper can additionally use up to 768 MiB. Extra RAM is a ceiling, not a reservation.

On this Core Ultra 5 125H, OpenVINO detected **Intel AI Boost** and a device-specific inference test reported **NPU** execution. The same 89-second captioned test completed on the NPU path without fallback in **80.61 seconds**, with **1505.78 MiB** peak sampled working memory and **1.87 CPU equivalents** peak sampled CPU, including thermal waits. The earlier CPU run took 57.46 seconds; these runs had different thermal conditions. NPU offload is working, but lower total energy/heat or faster end-to-end output has not been established by these measurements.

### Verified on this laptop (2026-09-15)

- Real YouTube link → download/merge → transcription → two captioned 1080p MP4s; browser playback and ZIP download passed.
- An 89.14-second uploaded video used two transcription chunks and produced 44.67/44.48-second clips in **57.46 seconds**. Sampled peak working memory was **443.94 MiB**, sampled peak CPU **2.0 CPU equivalents**. This is one small benchmark, not a guarantee for every source/model.
- Real thermal pauses, queued cancellation, and restart during a split-layout render passed; completed clips were retained without duplicates.
- Piper synthesized a 2.58-second speech sample in its bounded container. Both services passed health checks. Native-run job downloads remained accessible after migration.
- Go race tests/vet, seven Python tests, and full MP4 decode checks passed. Exact visual equivalence to the reference was not reviewed.

## Podcast Shorts

Paste a YouTube video URL in the **Podcast Shorts** tab. The app downloads the video, transcribes its original speech locally, chooses cuts near sentence/pause boundaries, and renders the entire source into as many Shorts as needed.

- Every output is **at most 45 seconds**, including its audio/container duration.
- **No clip-count cap.** The final shorter part is kept. This is whole-video coverage, not a top-N highlight selector.
- 1080 × 1920 H.264/AAC MP4s with the original audio and word-highlighted captions.
- Full picture over a blurred background, center crop, or stacked left/right halves for a two-person wide shot. Layouts are fixed; there is no automatic active-speaker tracking.
- Local transcription using Intel NPU/OpenVINO or faster-whisper CPU int8; no API key or TTS service needed. `base` is the fast default; `small` uses CPU and improves recognition, especially for multilingual podcasts, at higher processing cost.
- Intel VAAPI encoding when available, with CPU fallback. One podcast at a time limits RAM use.
- Persistent queue, progress, cancellation, individual previews/downloads, and a streaming ZIP download containing all clips and a timestamp/transcript manifest.
- Jobs resume after server restarts using cached downloads/transcripts and completed clips. Closing the browser does not interrupt processing.

### Native development (Docker limits do not apply)

```bash
cd /home/human/THINGS/LOL/AI-Content-Farm
bash scripts/run-local.sh
```

Open **http://localhost:8080**. Dependencies and a local Go toolchain have been installed in `.venv/` and `.tools/`. The launcher rebuilds the server before starting it. Stop with Ctrl+C.

On a fresh Linux checkout, install Go 1.25+, Python 3.10+ with venv support, FFmpeg/ffprobe with libass, a font such as DejaVu Sans, and **Deno 2.3+ or Node 22+** for YouTube extraction. Then:

```bash
python3 -m venv .venv
.venv/bin/pip install -r requirements-shorts.txt
cp .env.example .env
bash scripts/run-local.sh
```

The first captioned job downloads Whisper model weights. Outputs are in `data/generated/`; in-progress downloads and transcript caches are in `data/shorts/`; model weights are in `data/models/`. Long podcasts require time and disk space proportional to their length. In-progress caches are retained for recovery; completed YouTube source downloads are removed by default.

If YouTube asks for sign-in, set `YOUTUBE_COOKIES_BROWSER=firefox` (or your browser) in `.env` and restart, or upload a downloaded podcast in **Asset Library**, then select it in Podcast Shorts. For Docker, mount a Netscape-format cookie file and set `YOUTUBE_COOKIES_FILE` to its container path. Update the downloader when YouTube changes: `.venv/bin/pip install -U 'yt-dlp[default]'`.

### API

```bash
curl -X POST http://localhost:8080/api/shorts \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://www.youtube.com/watch?v=VIDEO_ID","max_duration":45,"layout":"fit"}'
```

Returns `202` immediately with a job ID. `GET /api/shorts` lists jobs; `GET /api/shorts/{id}` returns progress and clips; `POST /api/shorts/{id}/cancel` cancels; `GET /api/shorts/{id}/download` downloads a ZIP. For local input use `"source":"podcast.mp4"` relative to the video library instead of `url`. Optional fields: `language` (e.g. `en`, `hi`, `te`), `no_captions`, `layout` (`fit`, `crop`, `split`). The legacy `/api/videos/import-youtube` endpoint now queues this same workflow.

### Hardware checked on 2026-09-15

Intel Core Ultra 5 125H (14 cores / 18 threads, up to 4.5 GHz), integrated Intel Arc graphics with working i915/VAAPI H.264 encoding, Intel AI Boost NPU verified with OpenVINO, 16 GB RAM, 16 GB swap, SK hynix BC901 512 GB NVMe (~393 GB free at initial inspection), Debian 13.6 / kernel 6.12.101. The Intel GPU handles encoding; the NPU overlay offloads Whisper inference through OpenVINO, with faster-whisper CPU fallback.

### Checks

```bash
go test -race ./...
go vet ./...
.venv/bin/python -m unittest discover -s scripts -p 'test_*.py'
```

## Included Features

- Prompt-based script generation via OpenAI-compatible API.
- Manual script approval/edit before render (`script_override`).
- Orientation control: `portrait`, `landscape`, `square`, `original`, `custom`.
- Background video library selection and upload from UI.
- Runtime settings API (`/api/settings`) persisted in SQLite.
- Job history persisted in SQLite.
- Mobile-first web UI served at `/`.

## Core Endpoints

- `GET /healthz`
- `POST /v1/scripts/generate`
- `POST /v1/jobs`
- `GET /v1/jobs`
- `GET /v1/jobs/{id}`
- `GET /api/settings`
- `PUT /api/settings`
- `GET /api/videos`
- `POST /api/videos/upload`

## Quick Start

```bash
bash scripts/docker-start.sh
```

Open: `http://localhost:8080`

The launcher detects Intel GPU access. The optional original Piper studio service starts with `bash scripts/docker-start.sh --studio`; podcast Shorts do not need it.

## Environment

See `.env.example` for all options. Important keys:

- `DB_PATH` SQLite DB path for jobs/settings.
- `INPUT_VIDEOS_DIR` folder containing source/background videos.
- `OUTPUT_VIDEOS_DIR` folder where rendered videos are written.
- `LLM_API_KEY` key for script generation.
- `TTS_PROVIDER` one of `piper`, `elevenlabs`, `auto`.
- `ELEVENLABS_API_KEY` and `ELEVENLABS_VOICE_ID` when using ElevenLabs.
- `TTS_DOCKER_SERVICE_NAME` docker container name for local Piper (`aicf-tts` by default).

### TTS Provider Modes

- `piper`: Always use local Piper (manual local mode).
- `elevenlabs`: Use ElevenLabs first; if credits are exhausted, the app auto-switches provider to `piper` and retries.
- `auto`: Try ElevenLabs first, then fallback to Piper for synthesis/preview when ElevenLabs fails.

When running in ElevenLabs mode, you can stop local TTS container:

```bash
docker compose stop tts
```

When running the API inside Docker, automatic TTS start/stop requires Docker socket access in the API container.

## Notes

- If `LLM_API_KEY` is empty, the app uses fallback local script generation.
- Generated videos are accessible via `/outputs/<filename>`.
