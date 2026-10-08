# Editable full-video Shorts

## Objective and acceptance

The Make Shorts workflow converts an entire podcast into sequential, upload-ready
1080×1920 H.264/AAC clips. The user confirmed both batch and individual editing.

- Offer 15/30/40/45-second presets and custom lengths from 5–45 seconds.
- Fixed-length cuts or sentence-aware cuts cover every source interval without
  gaps, overlaps, or a clip-count limit; retain the final shorter part. Reserve
  80 ms for frame/audio muxing so exported media stays within the selected cap.
- Reel style adds gentle moving zoom, contrast/color enhancement, a vignette,
  normalized speech, and animated word-highlighted captions. Clean style retains
  the existing appearance. Original speech and the entire source remain intact.
- Completed batches can be re-rendered with new duration/layout/style/captions.
  Each clip can be re-rendered from a new source start time and duration.
  Edits create a new job and never overwrite previous exports.
- Reuse the original source and transcript cache across edits. Retain YouTube
  sources by default; explicit `SHORTS_KEEP_SOURCE=false` requires redownloading.
- Native labeled controls and a modal editor fit the existing dark studio UI,
  retain values on errors, restore focus, and work on mobile and by keyboard.

## API contract

`POST /api/shorts` adds optional `cut_mode` (`sentence` default, `fixed`),
`edit_style` (`clean` default, `reel`), and `clip_start` (nonnegative seconds).
Omit `clip_start` for full-source coverage. When present, render one interval,
bounded by source end and the requested `max_duration` minus muxing reserve.

`POST /api/shorts/{id}/regenerate` accepts a complete set of render options using
the same fields, but source identity always comes from the terminal parent job.
Returns 202 and a new Job; errors use the existing `{ "error": "..." }` shape.
It is a create operation: repeat submissions create separate versions.
`source_job_id` identifies the original job that owns the shared source cache.
Existing callers retain sentence/clean defaults. Existing SQLite JSON payloads
need no schema migration.

## Implementation order and checks

1. Extend Go validation, queue creation, cache lineage, and regeneration route.
   Verify persisted independent jobs, inherited source, and invalid options.
2. Extend Python segmentation and FFmpeg/ASS rendering. Verify full two-hour
   coverage at every preset, edited intervals, escaping, and actual media output.
3. Add duration/style controls and a shared batch/clip editor. Verify submitted
   payloads, error recovery, focus, responsive layout, and downloads in Playwright.

## Structure, style, and commands

Keep queue logic in `internal/shorts/service.go`, HTTP in
`internal/httpserver/shorts.go`, media in `scripts/shorts.py`, and native HTML/CSS/JS
in `internal/httpserver/web/`. Tests live beside Go/Python code and in
`tests/browser/`. Follow existing Go formatting and Python/JS conventions:

```python
clips = plan_clips(duration, words, maximum, cut_mode)
```

Commands: `go test -p=1 -race ./...`, `go vet -p=1 ./...`,
`python3 -m unittest discover -s scripts -p 'test_*.py'`,
`STUDIO_RUN_MEDIA_TESTS=1 python3 scripts/test_shorts.py`,
`PLAYWRIGHT_BASE_URL=http://127.0.0.1:8080 npm run test:browser`.
Build: `go build -p=1 -o bin/api ./cmd/api`. Start: `bash scripts/run-local.sh`.

Always validate inputs and preserve old media; reuse installed dependencies.
Ask before unrelated infrastructure changes. Never commit credentials or
silence failing checks. No automatic publishing, active-speaker tracking, or
generated B-roll is implied. Gemini timing remains approximate and reviewable.
