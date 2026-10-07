# Runtime guide

AI Content Farm runs its web application, planning graph, media assembly, and job
queue locally. Gemini performs AI planning, speech generation, and captioned
Shorts transcription in the cloud. No local neural model is loaded or downloaded
in the default configuration. The optional no-key presenter demo uses lightweight
eSpeak synthesis.

## Start the studio

With Docker Engine, Compose v2.24+, and Buildx installed:

```bash
cp .env.example .env
# Set GEMINI_API_KEY in .env for cloud planning, speech and transcription.
bash scripts/docker-start.sh
```

Open **http://localhost:8080**. The launcher enables Intel GPU access when
`/dev/dri/renderD128` exists. NPU models and the Piper container are disabled
unless `ALLOW_LOCAL_MODELS=true` is explicitly configured. The launcher waits
for a hot CPU to cool before building. BuildKit runs in a separate container
with a one-CPU and 2 GiB budget.

For native development, install Go 1.25+, Python 3.11+, FFmpeg/ffprobe, DejaVu
fonts, and eSpeak NG. YouTube extraction additionally needs Deno 2.3+ or Node
22+.

```bash
python3 -m venv .venv
.venv/bin/pip install -r requirements-studio.lock -r requirements-shorts.txt
cp .env.example .env
bash scripts/run-local.sh
```

The native launcher loads `.env`, selects the virtual environment, limits
numerical-library threads, and builds Go with one worker. Native execution does
not provide Docker's hard CPU/RAM limit. Stop it with Ctrl+C. `npm run start`
is a convenience alias; Node is not needed to serve the frontend.

## Cloud configuration

```dotenv
GEMINI_API_KEY=your-key
GEMINI_MODEL=gemini-2.5-flash
GEMINI_TRANSCRIPTION_MODEL=gemini-2.5-flash
GEMINI_TTS_MODEL=gemini-2.5-flash-preview-tts
GEMINI_TTS_VOICE_A=Puck
GEMINI_TTS_VOICE_B=Kore
STUDIO_TTS_PROVIDER=gemini
SHORTS_TRANSCRIBER=gemini
ALLOW_LOCAL_MODELS=false
```

Keep keys in ignored `.env`; they are never sent to browser JavaScript. Cloud
requests use the provider's quota and billing. Captioned Shorts send audio to
Gemini. Choose eSpeak for offline presenter narration, or disable Shorts
captions when no transcription call is desired. A selected cloud service failure
is visible and does not start a local model.

If `STUDIO_TTS_PROVIDER` is unset, the renderer chooses Gemini when
`GEMINI_API_KEY` is configured, otherwise eSpeak. Each reviewed presenter job can
override that choice with `voice_provider`. Gemini and Piper caption timings are
estimated. eSpeak word events provide caption timing when available.

## Resource limits and cooling

| Component | Default CPU budget | Default memory budget |
| --- | --- | --- |
| Main Docker service | One CPU equivalent; one heavy job at a time | 2 GiB, no container swap |
| BuildKit container | One CPU equivalent | 2 GiB, no container swap |
| Optional Piper service, disabled by default | 0.75 CPU | 768 MiB, no container swap |

Presenter rendering uses one FFmpeg encoder thread, reduced process priority,
and `STUDIO_RESOURCE_MODE=gentle` by default:

| Format | Gentle preview, 12 fps | Full export, 24 fps |
| --- | --- | --- |
| Portrait | 360 × 640 | 1080 × 1920 |
| Landscape | 640 × 360 | 1920 × 1080 |
| Square | 360 × 360 | 1080 × 1080 |

Review a preview before requesting full resolution. `STUDIO_RESOURCE_MODE=standard`
changes previews to a 540-pixel short edge at 18 fps; it does not increase the
encoder thread count.

Both presenter and Shorts workflows pause at **75°C** and resume at **70°C**
when a readable Linux CPU sensor is available. Presenter checks run before
narration, between scenes, and at five-second checkpoints during longer frame
loops. Shorts check their work boundaries and rest for five seconds between
clips. Cooling appears in job progress. Configure presenter thresholds with
`STUDIO_PAUSE_TEMP_C`/`STUDIO_RESUME_TEMP_C`, and Shorts thresholds with
`SHORTS_PAUSE_TEMP_C`/`SHORTS_RESUME_TEMP_C`. Presenter thresholds cannot disable
the check. If sensors are unavailable, jobs report that limitation and retain
their resource limits. These checks are not a hardware temperature guarantee.

Downloads prefer sources up to 1080p/30 fps, use one fragment at a time, and are
limited to 5 MiB/s by default. At least 5 GiB free disk is required for Shorts.
Completed YouTube jobs remove downloaded originals unless
`SHORTS_KEEP_SOURCE=true`; uploaded library videos and finished outputs remain.

## Presenter casts

Cog & Axiom are the default original pair. Cog is a copper workshop robot;
Axiom is an ivory geometric scientist. Nova & Atlas remain available. Original
art includes idle, speaking, and blink poses.

The Cast view imports two names, roles, idle PNGs, and optional speaking/blink
PNGs. The HTTP upload limit is **4 MiB per image and 16 MiB total**. Files are
validated and decoded before the pair becomes available. The registry stores
imports in `data/cast/<id>/cast.json`; use `STUDIO_CAST_DIR` to select another
location. Names and roles are shared with the planner.

Direct filesystem manifests accept PNGs up to 8 MiB and 4096 pixels per side;
paths must be local basenames. URLs, traversal, and symlinks are rejected.

```bash
python scripts/studio_cast.py --validate /path/to/staged/cast.json
python scripts/studio_cast.py --list
```

A single-still import uses audio-driven motion without claiming mouth poses.
Personal Ryusui/Senku and Ryusui/Sai artwork can be imported into an ignored
local directory; no copyrighted character media or voice references are bundled
in the public repository. See [cast setup](../assets/presenters/README.md).

## Long video to Shorts

Open **Make Shorts**, upload a source or paste a YouTube video URL, select
fit/crop/split layout and caption preference, then start the job. Gemini
transcribes source chunks; the worker chooses sentence/pause boundaries and
covers the source sequentially. Output is 1080 × 1920 H.264/AAC with the original
audio, downloadable individually or in a ZIP. This workflow does not rank viral
moments or track active speakers. Review cloud-estimated caption timing.

Intel VAAPI encoding is used when available, with a single-thread CPU fallback.
One heavy job runs at a time. Transcription and clip checkpoints persist across
retries. Presenter jobs restart their render from the beginning after recovery.

```bash
curl -X POST http://localhost:8080/api/shorts \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://www.youtube.com/watch?v=VIDEO_ID","layout":"fit"}'
```

`GET /api/shorts/{id}` reports progress; `POST /api/shorts/{id}/cancel` cancels;
`GET /api/shorts/{id}/download` downloads the ZIP. For an uploaded source, use
`"source":"podcast.mp4"` relative to the video library. `no_captions:true` skips
transcription. If YouTube requires sign-in, upload the downloaded source or
configure `YOUTUBE_COOKIES_FILE`; native users can also set
`YOUTUBE_COOKIES_BROWSER=firefox`. Credentials and cookies stay outside Git.

## Optional local model integrations

These integrations are retained for capable machines and require explicit
`ALLOW_LOCAL_MODELS=true`. They are unnecessary for the cloud workflow.

Piper requires its service and downloaded voice models:

```dotenv
ALLOW_LOCAL_MODELS=true
STUDIO_TTS_PROVIDER=piper
STUDIO_VOICE_A=en_US-lessac-medium
STUDIO_VOICE_B=en_US-ryan-medium
```

```bash
bash scripts/docker-start.sh --studio
```

Docker uses `http://tts:5002`; native runs can use
`TTS_BASE_URL=http://localhost:5002`. CPU Whisper requires optional transcription
dependencies and `SHORTS_TRANSCRIBER=cpu`; the NPU overlay uses OpenVINO with
`SHORTS_TRANSCRIBER=auto` or `npu`. Those settings can download model weights and
increase memory/CPU use. They should remain disabled on a weak or hot machine.
The old NPU/Whisper measurements are historical evidence, not the default runtime
or a performance promise for this cloud-first configuration.

## Data, diagnostics, and legacy tools

Persistent state lives in `data/` and `videos/`. Presenter MP4/JPEG/SRT/JSON files
are available through each completed job. Shorts caches and checkpoints live
under `data/shorts/`; finished Shorts live under `data/generated/`. Imported
casts live under `data/cast/`. Personal cast assets are ignored by Git and the
Docker build context.

The original narrated-video tools remain at **/legacy.html**, with their
`/v1/scripts/generate`, `/v1/jobs`, library, and `/api/settings` endpoints.
Their `TTS_PROVIDER` setting is separate from presenter
`STUDIO_TTS_PROVIDER`. Docker auto-management is disabled by default and local
model services require explicit opt-in.

Lightweight checks:

```bash
go test -p=1 ./...
go vet -p=1 ./...
.venv/bin/python -m unittest discover -s scripts -p 'test_*.py'
```

Renderer media tests are deliberately opt-in. Run them sequentially only after
the machine has cooled; their production thermal checks remain active:

```bash
STUDIO_RUN_MEDIA_TESTS=1 .venv/bin/python scripts/test_studio_render.py
```

See [architecture](architecture.md) for recovery boundaries and
[verification](verification.md) for recorded results and remaining limitations.
