# Original studio cast

Cog and Axiom are original robot presenters: a curious copper tinkerer and a calm,
geometric scientist. Nova and Atlas remain available as an additional original
pair. All four have idle, speaking and blink expressions. Rebuild all PNGs with
`python assets/presenters/build_assets.py` (Pillow required). They are distributed
under the repository's GPL-3.0 license.

Custom pairs use the same registry as planning and rendering. Place `cast.json`
and PNGs in `data/cast/YOUR-PAIR/` (or `$STUDIO_CAST_DIR/YOUR-PAIR/`):

```json
{
  "id": "my-presenters",
  "name": "My presenters",
  "speakers": [
    {"id": "host", "name": "Host", "role": "Asks clear questions", "idle": "host.png"},
    {"id": "expert", "name": "Expert", "role": "Explains with examples", "idle": "expert.png", "talk": "expert-talk.png"}
  ]
}
```

There must be exactly two speakers. `idle` is required; `talk` and `blink` are
optional. Direct filesystem images are local PNG basenames, up to 8 MiB and
4096 pixels per side. The browser upload API has tighter limits: 4 MiB per image
and 16 MiB total.
Validate a staged import with `python scripts/studio_cast.py --validate
/path/to/cast.json`. The studio's upload form runs this validation before making
an imported pair available. Speaker roles also guide the planning worker.

Optional personal pairs are intentionally not bundled. To enable them locally,
place transparent PNGs under `assets/presenters/personal/`:

- `ryusui-idle.png`
- `senku-idle.png`
- `sai-idle.png`

Use artwork you own or are permitted to use. Optional `NAME-talk.png` and
`NAME-blink.png` files enable expressions. Without them, personal artwork uses
audio-driven bob and a waveform; no mouth animation is claimed or fabricated.
The renderer fits the entire image inside the same presenter frame, preserving
aspect ratio. Personal images are never downloaded automatically. Run
`python scripts/studio_render.py --capabilities` to check available pairs.

Import local stills with:

```sh
python scripts/import-personal-cast.py --ryusui /path/to/ryusui.png \
  --senku /path/to/senku.png --sai /path/to/sai.png
```

For opaque source images with a black backdrop, add
`--remove-black-background`; this removes only near-black regions connected to
the image corners. The import manifest records the transformation and still
animation mode. Images and the manifest stay inside the ignored `personal/`
directory. No voice references or profile configuration are read.
