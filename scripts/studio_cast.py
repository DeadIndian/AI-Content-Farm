#!/usr/bin/env python3
"""Shared, confined PNG sprite-pair registry for planning, rendering and uploads."""
from __future__ import annotations

import argparse
import filecmp
import json
import os
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / 'assets' / 'presenters'
ID_PATTERN = re.compile(r'^(?=.{1,64}$)[a-z][a-z0-9]*(?:-[a-z0-9]+)*$')
PNG_PATTERN = re.compile(r'^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}\.png$', re.IGNORECASE)
BUILTINS = {
    'cog-axiom': ('Cog & Axiom', [('cog', 'Cog', 'Curious hands-on tinkerer; asks concrete questions and tests useful analogies.'),
                                ('axiom', 'Axiom', 'Calm analytical scientist; explains precisely and checks assumptions.')]),
    'nova-atlas': ('Nova & Atlas', [('nova', 'Nova', 'Curious host; asks accessible questions and connects ideas to everyday life.'),
                                  ('atlas', 'Atlas', 'Thoughtful explainer; answers with clear examples and measured reasoning.')]),
}
PERSONAL = {
    'ryusui-senku': ('Ryusui & Senku', [('ryusui', 'Ryusui', 'Energetic curious host.'), ('senku', 'Senku', 'Analytical science explainer.')]),
    'ryusui-sai': ('Ryusui & Sai', [('ryusui', 'Ryusui', 'Ryusui asks practical questions.'), ('sai', 'Sai', 'Sai explains technical concepts carefully.')]),
}


def cast_root():
    return Path(os.environ.get('STUDIO_CAST_DIR', str(ROOT / 'data' / 'cast'))).resolve()


def _text(value, field, limit):
    if not isinstance(value, str) or not value.strip() or len(value) > limit or '\0' in value:
        raise ValueError(f'{field} must be nonempty text under {limit} characters.')
    return value.strip()


def _png(directory, filename, verify=True):
    if not isinstance(filename, str) or not PNG_PATTERN.fullmatch(filename) or Path(filename).name != filename:
        raise ValueError('Sprite images must be local PNG basenames, without folders or URLs.')
    source = directory / filename
    if source.is_symlink() or source.resolve().parent != directory.resolve():
        raise ValueError('Sprite symlinks and paths outside the cast directory are not allowed.')
    if not source.is_file():
        raise ValueError(f'Sprite file is missing: {filename}')
    if source.stat().st_size > 8 * 1024 * 1024:
        raise ValueError(f'Sprite exceeds the 8 MB file limit: {filename}')
    if verify:
        from PIL import Image
        with Image.open(source) as image:
            if image.format != 'PNG' or image.width < 32 or image.height < 32:
                raise ValueError('Sprites must be PNG images at least 32 pixels on each side.')
            if image.width > 4096 or image.height > 4096 or image.width * image.height > 20000000:
                raise ValueError('Sprite dimensions exceed 4096 pixels per side or 20 megapixels.')
            image.verify()
        with Image.open(source) as image:
            image.load()
    return str(source.resolve())


def validate_manifest(path, verify_images=True):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 32768:
        raise ValueError('Cast manifest must be a regular JSON file under 32 KB.')
    try:
        value = json.loads(path.read_text(encoding='utf-8'))
    except (ValueError, UnicodeError) as exc:
        raise ValueError('Cast manifest must contain valid UTF-8 JSON.') from exc
    if not isinstance(value, dict):
        raise ValueError('Cast manifest must be an object.')
    id_ = value.get('id')
    if not isinstance(id_, str) or not ID_PATTERN.fullmatch(id_):
        raise ValueError('Cast ID must start with a lowercase letter and contain only lowercase letters, numbers and hyphens.')
    if id_ in BUILTINS or id_ in PERSONAL:
        raise ValueError('That cast ID is reserved for an existing presenter pair.')
    name = _text(value.get('name'), 'Cast name', 80)
    speakers = value.get('speakers')
    if not isinstance(speakers, list) or len(speakers) != 2:
        raise ValueError('A presenter pair must have exactly two speakers.')
    normalized = []
    for speaker in speakers:
        if not isinstance(speaker, dict) or not isinstance(speaker.get('id'), str) or not ID_PATTERN.fullmatch(speaker['id']):
            raise ValueError('Each speaker needs a valid lowercase ID.')
        record = {'id': speaker['id'], 'name': _text(speaker.get('name'), 'Speaker name', 60),
                  'role': _text(speaker.get('role') or 'Conversational presenter.', 'Speaker role', 400)}
        for expression in ('idle', 'talk', 'blink'):
            if expression == 'idle' or speaker.get(expression):
                filename = speaker.get(expression)
                _png(path.parent, filename, verify_images)
                record[expression] = filename
        normalized.append(record)
    if normalized[0]['id'] == normalized[1]['id'] or normalized[0]['name'] == normalized[1]['name']:
        raise ValueError('The two speakers must have distinct IDs and names.')
    return {'id': id_, 'name': name, 'speakers': normalized}


def _finish(pair, builtin=False):
    available = all(Path(s['idle']).is_file() for s in pair['speakers'])
    animation = {}
    for speaker in pair['speakers']:
        idle, talk = Path(speaker['idle']), Path(speaker.get('talk', speaker['idle']))
        different = idle.is_file() and talk.is_file() and not filecmp.cmp(idle, talk, shallow=False)
        animation[speaker['name']] = 'mouth-poses-and-audio-motion' if different else 'audio-reactive-still'
    return {**pair, 'available': available, 'reason': '' if available else 'Import your own PNGs to enable this pair.',
            'builtin': builtin, 'presenter_animation': animation}


def _builtin(id_, records, personal=False):
    name, speakers = records[id_]
    directory = ASSETS / 'personal' if personal else ASSETS
    values = []
    for sid, sname, role in speakers:
        record = {'id': sid, 'name': sname, 'role': role, 'idle': str(directory / f'{sid}-idle.png')}
        for expression in ('talk', 'blink'):
            path = directory / f'{sid}-{expression}.png'
            if path.is_file(): record[expression] = str(path)
        values.append(record)
    return _finish({'id': id_, 'name': name, 'speakers': values}, builtin=True)


def get_pair(id_):
    if not isinstance(id_, str) or not ID_PATTERN.fullmatch(id_):
        raise ValueError('Unknown presenter pair.')
    if id_ in BUILTINS: return _builtin(id_, BUILTINS)
    if id_ in PERSONAL: return _builtin(id_, PERSONAL, personal=True)
    directory = cast_root() / id_
    if directory.is_symlink() or directory.resolve().parent != cast_root():
        raise ValueError('Custom cast directory must remain inside STUDIO_CAST_DIR.')
    path = directory / 'cast.json'
    if not path.is_file(): raise ValueError('Unknown presenter pair.')
    value = validate_manifest(path, verify_images=False)
    if value['id'] != id_: raise ValueError('Cast manifest ID does not match its directory.')
    for speaker in value['speakers']:
        for expression in ('idle', 'talk', 'blink'):
            if expression in speaker:
                speaker[expression] = _png(directory, speaker[expression], verify=False)
    return _finish(value)


def list_pairs():
    pairs = [get_pair(id_) for id_ in (*BUILTINS, *PERSONAL)]
    root = cast_root()
    if root.is_dir():
        for directory in sorted(root.iterdir()):
            if directory.is_dir() and not directory.is_symlink() and ID_PATTERN.fullmatch(directory.name) and directory.name not in (*BUILTINS, *PERSONAL):
                try:
                    pairs.append(get_pair(directory.name))
                except (OSError, ValueError):
                    # Partially uploaded or invalid manifests are never available to planners.
                    continue
    return pairs


def public_pair(pair):
    characters = []
    for speaker in pair['speakers']:
        characters.append({key: Path(value).name if key in ('idle', 'talk', 'blink') else value
                           for key, value in speaker.items()})
    return {**{key: value for key, value in pair.items() if key != 'speakers'},
            'speakers': [s['name'] for s in pair['speakers']], 'characters': characters}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--validate', type=Path, help='Validate a staged cast.json plus its PNG files')
    mode.add_argument('--list', action='store_true', help='List public pair metadata without filesystem paths')
    mode.add_argument('--pair', help='Resolve an internal pair including absolute sprite paths')
    args = parser.parse_args()
    try:
        result = validate_manifest(args.validate) if args.validate else get_pair(args.pair) if args.pair else [public_pair(p) for p in list_pairs()]
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (ValueError, OSError) as exc:
        print(f'Cast validation failed: {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
