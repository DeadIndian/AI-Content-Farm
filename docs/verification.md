# Verification record

## Editable Shorts — 2026-10-08

This pass used Linux, Go 1.25.0, Python 3.13.5, FFmpeg 7.1.5, and isolated
Playwright Chromium against the local Go server.

- Go race tests and `go vet -p=1 ./...` passed. New checks cover render-option
  validation, batch/single regeneration, source inheritance, cache lineage,
  preservation of original jobs, and the actual regeneration HTTP route.
- Python: **43 passed, 5 opt-in media tests skipped**. New checks cover full
  two-hour timeline coverage at 30/40/45 seconds, fixed vs sentence cuts,
  individual source offsets, shorter endings, sub-millisecond rounding residue,
  and escaped animated captions.
- Browser: **8 non-media journeys passed** (the corrected edit journey was
  rerun separately); 2 existing media journeys remained opt-in. New journeys
  exercise presets/custom durations, batch/individual edits, validation errors,
  retry, original export retention, and keyboard focus/Escape behavior. These
  new browser journeys mock job responses; Go tests cover the real HTTP route.
- Screenshots and overflow checks passed at 320, 768, 1024, and 1440 pixels.
  The mobile editor and Make Shorts page were visually inspected.
- `node --check internal/httpserver/web/studio.js`, the Go build, and
  `git diff --check` passed. No new application dependencies were added.

**Pending media verification:** the real Reel-render test reached the production
cooling guard at 103°C and timed out waiting for cooling. Follow-up readings were
100–104°C. The guard remains enabled. New FFmpeg effects, encoded clip durations,
and the complete media/download journey therefore still need verification on a
cool machine. Run `STUDIO_RUN_MEDIA_TESTS=1 .venv/bin/python scripts/test_shorts.py`
to check H.264/AAC, all three Reel layouts, and visibly burned captions. No live
YouTube or Gemini request was exercised in this pass; no Gemini key was supplied.

## Earlier verification

Recorded on 2026-10-07. The test machine uses Linux, Go 1.25.5 and Python 3.13.5. The configured application uses Gemini for planning, speech and transcription, with local models disabled and gentle rendering selected.

## Current local checks

- `go test -race -p=1 ./...` and `go vet -p=1 ./...` passed. Coverage includes persistent jobs, restart recovery, cancellation of subprocess groups, retries, output confinement and real PNG upload/validation/download through the HTTP handler.
- Python unit checks passed, including structured Gemini requests, provider failures, transcript timestamp validation, cached cloud chunk boundaries, rejection of local model loading, cast manifests, scene validation and LangGraph checkpoint persistence. Media tests are opt-in.
- Playwright: **6 passed, 2 media journeys skipped**. Desktop 1440px and mobile 390px navigation passed across all five views without JavaScript errors or horizontal overflow. Tests cover draft edits and voice selection across reload, provider failures, cloud/gentle defaults and cast upload error/retry UI. Browser cast responses are mocked; the Go HTTP test exercises the actual upload and image validator.
- Final desktop/mobile screenshots were captured against the running application and visually inspected.
- Shell syntax, loading `.env.example`, Docker Compose configuration and whitespace checks passed. The yt-dlp dependency is installed; no local neural-model dependency was installed or loaded.
- Candidate repository files were scanned against the configured credential and common Google API-key patterns. Credentials, personal anime artwork, private profile data and generated working directories are excluded from Git.

## Live cloud checks

- The running Go API executed a real Gemini request through all five LangGraph stages for Cog & Axiom. The final 30-second brief produced 55 spoken words with a 25-second estimate. Target duration remains guidance; synthesized audio determines the final runtime.
- Gemini speech returned usable 24 kHz PCM audio for a short original Cog introduction, approximately 4.01 seconds long.
- That speech sample was sent to Gemini transcription using the production request schema. Its real response passed the production parser with eight words and bounded timestamps. Network transport for this transcription probe used curl; parsing and request construction used the application module. Cloud timestamps are model estimates, not forced alignment.

## Media evidence and remaining checks

Earlier in the implementation, actual presenter outputs passed FFprobe and complete decoding: original portrait, landscape, square and imported Ryusui/Sai stills. A full portrait example contained H.264 video and AAC audio at 1080×1920/24 fps, ran 36.083 seconds and was about 1.67 MB. The original Nova/Atlas sample is committed under `docs/demo/`.

The earlier browser presenter journey produced, played and downloaded an actual MP4. The two-Shorts browser journey reached the production thermal pause on this machine and was cancelled. Following the owner's weak-machine constraint, final local checks do not launch video encodes or local models while the CPU is hot. The latest Gemini-narrated Cog/Axiom MP4 has therefore **not** been verified end to end locally.

The [GitHub Actions template](ci/verify-studio.yml) explicitly enables the renderer media tests, the real LangGraph → renderer → HTTP download integration and both browser media journeys on a remote Ubuntu runner. GitHub rejected adding an active workflow because the available OAuth credential lacks `workflow` scope. The template is preserved under `docs/ci/` and does not execute there. Remote media verification remains pending; no passing CI result is claimed. See [activation instructions](ci/README.md).

Docker Compose configuration was validated. A Docker image build could not be exercised locally because access to the Docker daemon was unavailable. Live Piper, CPU/NPU transcription, YouTube sign-in/cookie flows and third-party cloud chat providers were not exercised in this pass. Their requirements and opt-in behavior are documented.

## Reproduce

Run the lightweight commands from the README first. On a machine ready for encoding:

```bash
STUDIO_RUN_MEDIA_TESTS=1 .venv/bin/python -m unittest discover -s scripts -p 'test_*.py'
STUDIO_INTEGRATION_ROOT="$PWD" go test -p=1 ./internal/httpserver -run TestStudioRealPipeline -v
STUDIO_RUN_MEDIA_TESTS=1 PLAYWRIGHT_BASE_URL=http://127.0.0.1:8080 npm run test:browser
```

Start the application before browser checks. Tests retain the production thermal guards. CI uses eSpeak so media verification needs no API credentials.
