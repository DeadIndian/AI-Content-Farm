# Architecture and operational boundaries

## Two workflows, one studio

Static HTML/CSS/JavaScript is embedded in the Go binary and uses a same-origin API. The core app needs no Node server or frontend framework build. Node/Playwright are development tools.

Presenter authoring calls `POST /api/studio/drafts`. A Python LangGraph process validates the brief, prepares context, writes structured scenes through Gemini or an explicitly configured cloud API, validates the result, and records a review stage. Demo/manual/AI modes are explicit. Graph checkpoints live in `data/studio/planning.sqlite3`; there is no public planner-resume endpoint. Browser draft persistence preserves edits across reloads.

Rendering starts when a reviewed draft reaches `POST /api/studio/jobs`. Go persists jobs before processing. A shared gate permits one heavy job at a time across presenter, legacy, and Shorts workers. Each job can select a `voice_provider`; Gemini cloud speech and lightweight eSpeak are the default choices. Speech comes before frame timing. Pillow composites PNGs; audio energy drives speaking motion. FFmpeg encodes H.264/AAC with one encoder thread and reduced priority. Successful renders produce MP4, JPEG, SRT, and metadata JSON.

Shorts keep the existing `/api/shorts` engine: download/open source, transcribe chunks with Gemini, choose sentence/pause boundaries, reframe using fit/crop/split, and burn captions. The final shorter part is retained. This covers the whole input sequentially rather than ranking highlights. Disabling captions skips transcription. Local model providers are blocked unless `ALLOW_LOCAL_MODELS=true` is explicitly set.

`scripts/studio_cast.py` is the shared cast registry for planning, rendering, and imports. Cog & Axiom are the default original pair; Nova & Atlas remain available. Custom manifests under `STUDIO_CAST_DIR/<id>/cast.json` supply two names, roles, and confined PNG basenames. The upload API stages files, validates and decodes them, then publishes the pair. Optional speaking/blink expressions are used when supplied; still-only art is labeled as audio-driven motion. Roles guide dialogue planning, and uploaded images stay in persistent data rather than source control.

## Recovery and errors

- Presenter jobs move from queued to running, then completed/failed/canceled. Retrying failed/canceled work creates a new job and preserves its original record.
- Restarted presenter jobs rerender from the beginning. Shorts retain transcription/clip checkpoints. These are distinct recovery guarantees.
- Cancellation kills the Python process group, including FFmpeg. Shutdown also cancels active planning/capability subprocesses.
- Downloads become available only after successful completion. Fixed output filenames use Go `os.Root`, with traversal/symlink tests.
- AI failures are visible. Demo scripts never silently replace failed AI output. Selected speech providers do not silently switch voices.
- Trace records show node outcomes and timings, not hidden model reasoning.

## API map

| Method/path | Purpose |
| --- | --- |
| `GET /api/studio/capabilities` | Planner, renderer, voice, and cast readiness |
| `POST /api/studio/drafts` | Build an editable plan |
| `POST /api/studio/cast` | Validate and import a custom PNG presenter pair |
| `GET /api/studio/cast/{id}/{filename}` | Serve a confined imported sprite |
| `POST /api/studio/jobs` | Queue a reviewed draft |
| `GET /api/studio/jobs` | List presenter jobs |
| `GET /api/studio/jobs/{id}` | Status, trace, outputs |
| `POST /api/studio/jobs/{id}/cancel` | Cancel work |
| `POST /api/studio/jobs/{id}/retry` | Retry failed/canceled work |
| `GET /api/studio/jobs/{id}/files/{name}` | Video, poster, captions, metadata |
| `POST /api/shorts` | Queue YouTube/library repurposing |
| `GET /api/shorts/{id}/download` | Clips and manifest ZIP |
| `POST /api/videos/upload` | Add source media |

## Tradeoffs

This is a trusted local single-user application, not a public multi-tenant service. Authentication, quotas, isolation, and an access-controlled reverse proxy are needed before exposing it to untrusted users. Legacy filesystem settings assume a trusted operator.

Process-group cancellation targets Linux. Use Docker on other hosts. SQLite and one render slot keep local execution simple; distributed workers and object storage are absent.

Target length guides writing; actual duration follows synthesized speech. Default gentle previews use a 360-pixel short edge at 12 fps. Standard previews use a 540-pixel short edge at 18 fps; full exports use a 1080-pixel short edge at 24 fps. Gemini/Piper caption word positions and Gemini transcription timings are estimated. eSpeak word events time captions where available. Review caption alignment and factual claims before publication.

Thermal checks pause presenter and Shorts work at 75°C and resume at 70°C. Missing sensors are reported, and thread/container limits remain in effect. These checks reduce load but do not guarantee a hardware temperature ceiling. The [runtime guide](runtime-guide.md) describes cloud configuration, resource budgets, optional acceleration, and local model opt-in.
