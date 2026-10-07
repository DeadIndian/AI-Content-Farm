# Research and project lineage

Reviewed on 2026-10-07. External project descriptions below come from their repositories and documentation, not independent quality benchmarks.

## Existing work

| Source | Findings | Decision |
| --- | --- | --- |
| [AI-Content-Farm](https://github.com/DeadIndian/AI-Content-Farm) | Go API, SQLite settings/jobs, Python Shorts engine, Whisper, FFmpeg, resource gate, restart/cancel paths, legacy studio. | Keep as the main repository and retain the media infrastructure. |
| [Viral Faceless Shorts Generator](https://github.com/DeadIndian/MORE-DISGUSTING-Viral-Faceless-Shorts-Generator) | README describes Gemini scripts, manual approval, Piper, Aeneas alignment, and multiple services. | Preserve review-before-render and configurable narration as concepts. No predecessor source copied. |
| [yt-video-generation-experiment](https://github.com/DeadIndian/yt-video-generation-experiment) | Inspected architecture, tree, and pipeline.py: typed scenes, renderer dispatch, content hashes, output quality checks. The script-to-scene “Brain” is explicitly unimplemented in that revision. | Implement a real structured LangGraph planner; keep model output separate from execution. |
| [Downloader](https://github.com/DeadIndian/Downloader) | Adjacent mobile media-downloader project; README has little implementation detail. | Retain yt-dlp ingestion rather than adding a mobile dependency. |
| Hermes yt and script-gen profiles | Relevant memories, pipeline documentation, and asset listings show earlier Ryusui/Sai evolving into Ryusui/Senku. They emphasize character roles, readable illustrations, speaking motion, and audio-first timing. | Support both personal presets; ship original Cog/Axiom and Nova/Atlas artwork. No private memories, credentials, or voice references copied. |

The owner identified the fictional pair as Cog and Axiom. No matching sprites were found in the scoped searches, so this project ships newly drawn, reproducible artwork for them. The imported personal pairings are Ryusui/Sai and Ryusui/Senku; their assets stay outside Git.

## Related public projects

| Project | Relevant approach | Design implication |
| --- | --- | --- |
| [MoneyPrinterTurbo](https://github.com/harry0703/MoneyPrinterTurbo) · MIT | Topic-to-script, asset matching, speech providers, subtitles, and composition. README and metadata reviewed. | Make provider configuration and editable content first-class features. PNG conversations differentiate this from stock-footage assembly. |
| [AI YouTube Shorts Generator](https://github.com/Anil-matcha/AI-Youtube-Shorts-Generator) · MIT | README describes local/API modes, LLM highlight selection, transcript chunking, deduplication, and vertical cropping. | Highlight selection with editorial rationales is a useful extension. Sequential cuts should not be labeled “viral AI highlights.” |
| [EmoteSync](https://github.com/ElodineOfficial/EmoteSync) · MIT | Audio-driven PNG expressions, bounce animation, optional emotion models. Its README warns it is outdated. | Audio-driven presenter motion adds value without requiring a separate emotion model. |
| [MoneyPrinterV2](https://github.com/FujiwaraChoki/MoneyPrinterV2) · AGPL-3.0 | Broad content automation project; repository metadata reviewed. | Focus the showcase on authoring and measurable output rather than automated posting. |
| [LosslessCut](https://github.com/mifi/lossless-cut) · GPL-2.0 | Repository describes lossless video/audio editing. | Captioned reframing requires re-encoding; do not call that workflow lossless. |

No external source code or assets were imported. Licenses reflect GitHub metadata at review time.

## Why LangGraph

[LangGraph persistence documentation](https://docs.langchain.com/oss/python/langgraph/persistence) describes checkpointers for thread state and stores for data across threads. This app uses a StateGraph for brief → source context → script → validation → review, with SQLite planning checkpoints. Drafts then cross a browser review boundary before entering the render queue.

This is a bounded workflow. Model calls time out, scene data is validated, and only fixed rendering code executes. Rendering and job management remain useful when a model provider is unavailable. More agents are not automatically better.

## Extensions with measurable value

- Transcript highlight candidates with editable boundaries and evidence-backed rationales, evaluated against manually labeled moments.
- Motion-smoothed face tracking with a crop preview and manual fallback.
- Optional retrieval with source provenance and claim/source checks. Current source notes are supplied context, not verified research.
- Per-scene caching keyed by script, voice, assets, and renderer version. Presenter restart recovery currently restarts an interrupted render.

These are future work, not current product claims.
