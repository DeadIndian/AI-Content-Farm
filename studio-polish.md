# AI Content Farm studio implementation plan

## Accepted user direction (2026-10-07)

- Original cast plus configurable sprite presets; user confirmed Cog and Axiom as original fictional pair, Dr. Stone Ryusui/Sai as anime preset. Create new Cog/Axiom artwork if no existing assets found.
- Support configurable cloud and lightweight offline operation, but **no local model inference/downloads on this machine**. Use the authorized Gemini API key from the ADB project without displaying or committing it.
- Cloud Gemini for script generation and transcription, optional Gemini speech. Keep eSpeak as a lightweight non-model demo; Piper/Whisper only explicitly enabled on more capable installations.
- Default to one worker, one encoder thread, low-resolution previews, thermal pauses, and no parallel media tests. Complete all remaining verification without excessive machine load.
- Add sprite-pair import through the cast UI using a validated manifest/PNG contract shared by planner and renderer.

Keep this repository and its Go/SQLite/FFmpeg Shorts backend. Add a polished, embedded studio that brings topic-to-PNG-presenter videos and long-video repurposing into one workflow. Preserve the original studio at `/legacy.html`.

## Work streams

1. Inspect related GitHub projects and Hermes `yt` / `script-gen` profiles; record reusable concepts and online comparisons with source links.
2. Add a Python LangGraph planning worker with structured scene scripts, explicit demo/manual/AI modes, bounded provider calls, trace records, and editable review before rendering.
3. Add durable Go studio jobs sharing the existing heavy-work gate, status/cancel/retry, validated requests, downloadable video/subtitles, and capability checks.
4. Build a deterministic PNG presenter renderer: original reusable characters, alternating dialogue, audio-driven motion, readable scene cards, captions, portrait/landscape output, and real FFmpeg MP4 generation. Support optional personal character presets from local assets.
5. Build a responsive studio UI with Create, Shorts, Projects, Cast, and Settings views. Every action must call a working backend or clearly explain a missing prerequisite. No fabricated job statistics or fake AI claims.
6. Verify existing Go/Python checks; exercise new planning/rendering/recovery/error paths, actual MP4 decoding, desktop/mobile keyboard/browser flows, and packaging. Document installation and demo limitations precisely.
7. Commit and push reviewed work to AI-Content-Farm without rewriting existing history.

## Interface contract

- `GET /api/studio/capabilities`: provider/renderer readiness, presenter pairs, limitations.
- `POST /api/studio/drafts`: `{topic, source_notes, pair, format, target_seconds, provider}` where provider is `demo`, `manual`, or `ai`.
- Draft: `{title, topic, pair, format, provider, scenes:[{speaker:0|1,text,visual}], sources:[{title,url}], warnings:[], trace:[{node,status,detail,duration_ms}], estimated_seconds}`.
- `POST /api/studio/jobs`: `{draft, quality:"preview"|"full"}`; `GET /api/studio/jobs`; `GET /api/studio/jobs/{id}`; `POST /api/studio/jobs/{id}/cancel`; `POST /api/studio/jobs/{id}/retry`.
- Job: `{id,status,stage,progress,message,created_at,updated_at,request,output_url,poster_url,caption_url,trace,error}`. Status: `queued`, `running`, `completed`, `failed`, `canceled`.
- Python planner CLI accepts a JSON request on stdin and emits a JSON draft to stdout.
- Python renderer CLI `scripts/studio_render.py --input REQUEST_JSON --output OUTPUT_MP4` accepts `{draft,quality}`. Writes adjacent `.jpg`, `.srt`, and `.json` metadata. Progress JSON lines on stdout. Failures exit nonzero.
- Existing Shorts endpoints remain the source of truth for repurposing, including status, cancellation, videos upload, output URLs, and ZIP download.

## Acceptance

A fresh local demo can produce and play an actual presenter video without paid API keys; AI topic generation is explicitly opt-in and never silently replaced with a generic script. A source video can produce multiple downloadable Shorts. Errors are visible and retryable. No credentials, private profile data, or unlicensed personal media are committed.
