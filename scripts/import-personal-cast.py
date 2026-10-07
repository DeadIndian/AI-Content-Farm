#!/usr/bin/env python3
"""Import explicitly supplied local still artwork into the ignored personal cast.

No files are downloaded, and no voice references, credentials, or profile
configuration are read. A still remains a still: no mouth poses are fabricated.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import sys

from PIL import Image, ImageChops, ImageDraw, ImageOps

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / 'assets' / 'presenters' / 'personal'


def remove_edge_black(image):
    """Remove only near-black regions connected to image corners, preserving detail."""
    red, green, blue = image.convert('RGB').split()
    maximum = ImageChops.lighter(ImageChops.lighter(red, green), blue)
    mask = maximum.point(lambda value: 255 if value <= 20 else 0)
    for corner in ((0, 0), (image.width-1, 0), (0, image.height-1), (image.width-1, image.height-1)):
        if mask.getpixel(corner) == 255:
            ImageDraw.floodfill(mask, corner, 128)
    alpha = mask.point(lambda value: 0 if value == 128 else 255)
    image.putalpha(ImageChops.darker(image.getchannel('A'), alpha))
    return image


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('ryusui', 'senku', 'sai'):
        parser.add_argument('--'+name, type=Path, help=f'Local {name.title()} PNG artwork')
    parser.add_argument('--remove-black-background', action='store_true',
                        help='Remove corner-connected near-black backgrounds from opaque source images')
    parser.add_argument('--overwrite', action='store_true', help='Replace an existing imported idle PNG')
    args = parser.parse_args(argv)
    supplied = [(name, getattr(args, name)) for name in ('ryusui', 'senku', 'sai') if getattr(args, name)]
    if not supplied:
        parser.error('Supply at least one local PNG with --ryusui, --senku, or --sai.')
    DEST.mkdir(parents=True, exist_ok=True)
    manifest_path = DEST / 'cast.json'
    manifest = json.loads(manifest_path.read_text()) if manifest_path.is_file() else {'schema_version': 1, 'characters': {}}
    for name, source in supplied:
        destination = DEST / f'{name}-idle.png'
        if destination.exists() and not args.overwrite:
            raise ValueError(f'{name.title()} is already installed; use --overwrite to replace it.')
        with Image.open(source) as original:
            if original.format != 'PNG':
                raise ValueError(f'{source.name} must be a PNG file.')
            if original.width * original.height > 20000000:
                raise ValueError(f'{source.name} exceeds the 20 megapixel source limit.')
            original.load()
            opaque = 'A' not in original.getbands() or original.getchannel('A').getextrema() == (255, 255)
            image = original.convert('RGBA')
        removed = bool(args.remove_black_background and opaque)
        if removed:
            image = remove_edge_black(image)
        image = ImageOps.contain(image, (1600, 1800), Image.Resampling.LANCZOS)
        image.save(destination, optimize=True)
        manifest['characters'][name] = {'source_filename': source.name,
                                       'imported_at': datetime.now(timezone.utc).isoformat(),
                                       'animation': 'audio-reactive-still', 'expressions': ['idle'],
                                       'width': image.width, 'height': image.height,
                                       'edge_black_background_removed': removed}
        print(f'Imported {name.title()}: original still artwork, audio-driven bob and waveform; no mouth poses.')
    manifest_path.write_text(json.dumps(manifest, indent=2)+'\n', encoding='utf-8')
    print('Personal images and metadata remain ignored by Git.')
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError) as exc:
        print(f'Import failed: {exc}', file=sys.stderr)
        raise SystemExit(1)
