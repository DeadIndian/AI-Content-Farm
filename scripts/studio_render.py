#!/usr/bin/env python3
"""Render reviewed dialogue into real, narrated PNG-presenter videos.

The default offline mode needs no credentials or network and uses eSpeak NG
(binary or shared library). STUDIO_TTS_PROVIDER=piper opts into the configured
local Piper HTTP service. Offline speech is labeled synthetic demo narration. Frames
are streamed to one resource-bounded FFmpeg process; temporary media is removed
on errors and termination. Assets and script content are never fetched here.
"""
from __future__ import annotations

import argparse
import array
import base64
import bisect
import ctypes
import ctypes.util
import filecmp
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen
import wave
import studio_cast

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / 'assets' / 'presenters'
FORMATS = {'portrait': (1080, 1920), 'landscape': (1920, 1080), 'square': (1080, 1080)}
COLORS = {'bg': '#F4EFE5', 'ink': '#213C43', 'muted': '#667979', 'mint': '#DCE8DF',
          'line': '#D5DFD5', 'coral': '#DE7962', 'teal': '#2E726D', 'paper': '#FFFCF4'}
_ACTIVE_PROCESS = None
_CANCELED = False
_THERMAL_WARNED = False
_PRIORITY_SET = False
GEMINI_VOICES = {'Zephyr', 'Puck', 'Charon', 'Kore', 'Fenrir', 'Leda', 'Orus', 'Aoede',
                 'Callirrhoe', 'Autonoe', 'Enceladus', 'Iapetus', 'Umbriel', 'Algieba',
                 'Despina', 'Erinome', 'Algenib', 'Rasalgethi', 'Laomedeia', 'Achernar',
                 'Alnilam', 'Schedar', 'Gacrux', 'Pulcherrima', 'Achird', 'Zubenelgenubi',
                 'Vindemiatrix', 'Sadachbia', 'Sadaltager', 'Sulafat'}


def progress(stage, value, message):
    print(json.dumps({'stage': stage, 'progress': round(value), 'message': message}), flush=True)


def default_voice_provider():
    return (os.environ.get('STUDIO_TTS_PROVIDER') or ('gemini' if os.environ.get('GEMINI_API_KEY') else 'espeak')).strip().lower()


def voice_engine(provider=None):
    provider = (provider or default_voice_provider()).strip().lower()
    if provider == 'gemini':
        return 'gemini-cloud', os.environ.get('GEMINI_API_KEY')
    if provider == 'piper':
        if os.environ.get('ALLOW_LOCAL_MODELS', 'false').strip().lower() != 'true':
            return 'piper-blocked', None
        return 'piper-http', os.environ.get('TTS_BASE_URL', 'http://localhost:5002').rstrip('/')
    if provider != 'espeak':
        return 'unavailable', None
    lib = ctypes.util.find_library('espeak-ng') or ctypes.util.find_library('espeak')
    if lib:
        return 'espeak-library', lib
    cmd = shutil.which('espeak-ng') or shutil.which('espeak')
    return ('espeak-command', cmd) if cmd else ('unavailable', None)


def asset_path(name, expression, personal=False):
    return ASSETS / ('personal' if personal else '') / f'{name.lower()}-{expression}.png'


def animation_mode(name, personal=False):
    idle, talk = asset_path(name, 'idle', personal), asset_path(name, 'talk', personal)
    if not talk.is_file() or (idle.is_file() and filecmp.cmp(idle, talk, shallow=False)):
        return 'audio-reactive-still'
    return 'mouth-poses-and-audio-motion'


def temperature():
    readings = []
    for label in Path('/sys/class/thermal').glob('thermal_zone*/type'):
        try:
            if label.read_text().strip() in ('x86_pkg_temp', 'cpu-thermal', 'cpu_thermal'):
                readings.append(float(label.with_name('temp').read_text()) / 1000)
        except (OSError, ValueError):
            pass
    for label in Path('/sys/class/hwmon').glob('hwmon*/name'):
        try:
            if label.read_text().strip() in ('coretemp', 'k10temp', 'zenpower', 'cpu_thermal'):
                for sensor in label.parent.glob('temp*_input'):
                    readings.append(float(sensor.read_text()) / 1000)
        except (OSError, ValueError):
            pass
    return max(readings) if readings else None


def thermal_thresholds():
    pause = float(os.environ.get('STUDIO_PAUSE_TEMP_C', os.environ.get('SHORTS_PAUSE_TEMP_C', '75')))
    resume = float(os.environ.get('STUDIO_RESUME_TEMP_C', '70'))
    if not 40 <= resume < pause <= 95:
        raise ValueError('Thermal thresholds must satisfy 40 <= resume < pause <= 95 Celsius; cooling checks cannot be disabled.')
    return pause, resume


def cool_down(value=0):
    global _THERMAL_WARNED
    pause, resume = thermal_thresholds()
    current = temperature()
    if current is None:
        if not _THERMAL_WARNED:
            progress('resource-check', value, 'CPU sensor unavailable; gentle rendering and a single encoder thread remain active.')
            _THERMAL_WARNED = True
        return
    if current < pause:
        return
    while current is not None and current > resume:
        progress('cooling', value, f'CPU {current:.0f}°C. Rendering is paused until it cools to {resume:.0f}°C.')
        time.sleep(3)
        current = temperature()
    progress('resource-check', value, 'CPU cooled; resuming the queued render.')


def resource_settings(quality, format):
    mode = os.environ.get('STUDIO_RESOURCE_MODE', 'gentle').strip().lower()
    if mode not in ('gentle', 'standard'):
        raise ValueError('STUDIO_RESOURCE_MODE must be gentle or standard.')
    width, height = FORMATS[format]
    if quality == 'preview':
        divisor, fps = (3, 12) if mode == 'gentle' else (2, 18)
        width //= divisor; height //= divisor
    else:
        fps = 24
    return width, height, fps, mode


def low_priority():
    global _PRIORITY_SET
    if _PRIORITY_SET:
        return
    _PRIORITY_SET = True
    try:
        if hasattr(os, 'nice'):
            current = os.getpriority(os.PRIO_PROCESS, 0)
            if current < 10: os.nice(10-current)
    except (OSError, AttributeError):
        pass


def piper_readiness():
    """Probe an explicitly enabled service without starting or downloading models."""
    if os.environ.get('ALLOW_LOCAL_MODELS', 'false').strip().lower() != 'true':
        return False, 'Local models are disabled. Piper requires explicit ALLOW_LOCAL_MODELS=true.'
    location = os.environ.get('TTS_BASE_URL', 'http://localhost:5002').rstrip('/')
    try:
        valid_url = urlparse(location).scheme in ('http', 'https')
    except ValueError:
        valid_url = False
    if not valid_url:
        return False, 'TTS_BASE_URL must be an HTTP or HTTPS service URL.'
    try:
        with urlopen(location + '/healthz', timeout=2) as response:
            if response.status != 200:
                return False, 'Piper health check failed.'
    except (OSError, ValueError):
        # Do not expose URLs or credentials from transport exception strings.
        return False, 'Piper service could not be reached.'
    return True, ''


def capabilities(provider=None):
    pillow = importlib.util.find_spec('PIL') is not None
    selected = provider or default_voice_provider()
    engine, location = voice_engine(selected)
    voice = bool(location)
    voice_reason = ''
    piper_available, piper_reason = piper_readiness()
    if engine == 'gemini-cloud' and not voice:
        voice_reason = 'Gemini cloud speech requires GEMINI_API_KEY.'
    if engine == 'piper-blocked':
        voice_reason = piper_reason
    if engine == 'piper-http':
        voice = piper_available
        if not voice:
            voice_reason = 'Piper is selected but unavailable: ' + piper_reason
    if voice and engine == 'espeak-library':
        # A library can exist without its voice data. Probe initialization as well.
        try:
            speaker = Speech(selected)
            speaker.close()
        except Exception as exc:
            voice = False
            voice_reason = str(exc)
    pairs = [studio_cast.public_pair(pair) for pair in studio_cast.list_pairs()]
    ffmpeg = bool(shutil.which('ffmpeg'))
    missing = []
    if not ffmpeg: missing.append('Install FFmpeg')
    if not pillow: missing.append('Install Pillow from requirements-studio.txt')
    if not voice: missing.append(voice_reason or 'Select Gemini cloud speech or install lightweight eSpeak NG.')
    if not pairs[0]['available']: missing.append('Restore the original presenter PNG assets')
    providers = [
        {'id': 'gemini', 'name': 'Gemini cloud', 'available': bool(os.environ.get('GEMINI_API_KEY')),
         'reason': '' if os.environ.get('GEMINI_API_KEY') else 'Configure GEMINI_API_KEY.'},
        {'id': 'espeak', 'name': 'eSpeak · lightweight offline', 'available': bool(voice_engine('espeak')[1]),
         'reason': '' if voice_engine('espeak')[1] else 'Install lightweight eSpeak NG.'},
        {'id': 'piper', 'name': 'Piper · local model', 'available': piper_available, 'reason': piper_reason}]
    return {'ready': not missing, 'ffmpeg': ffmpeg, 'pillow': pillow, 'voice': voice,
            'voice_engine': engine, 'reason': '; '.join(missing), 'pairs': pairs,
            'voice_providers': providers, 'default_voice_provider': selected,
            'resource_mode': os.environ.get('STUDIO_RESOURCE_MODE', 'gentle'), 'encoder_threads': 1,
            'cpu_temperature_c': temperature(),
            'limitations': ['Piper needs its local service and selected voice models; first use can download models.' if engine == 'piper-http'
                            else 'Gemini speech runs in the cloud; provider charges and quotas apply.' if engine == 'gemini-cloud'
                            else 'Offline eSpeak narration is intelligible but robotic; review it before publishing.',
                            'Visuals are designed scene cards; external images are not downloaded.',
                            'Captions use eSpeak word events when available, otherwise estimated word timing.']}


class Speech:
    """Explicit eSpeak or Piper speech provider; eSpeak supplies word timings."""
    def __init__(self, provider=None):
        self.kind, location = voice_engine(provider)
        if not location:
            if self.kind == 'piper-blocked':
                raise RuntimeError('Piper is disabled because ALLOW_LOCAL_MODELS is not true.')
            if self.kind == 'gemini-cloud':
                raise RuntimeError('Gemini cloud speech requires GEMINI_API_KEY.')
            raise RuntimeError('Speech is unavailable. Configure Gemini or install lightweight espeak-ng.')
        self.command = location
        self.lib = None
        self.chunks = []
        self.words = []
        if self.kind == 'gemini-cloud':
            self.model = os.environ.get('GEMINI_TTS_MODEL', 'gemini-2.5-flash-preview-tts')
            if not re.fullmatch(r'[A-Za-z0-9._-]+', self.model):
                raise ValueError('GEMINI_TTS_MODEL must be a model identifier.')
            self.voices = [os.environ.get('GEMINI_TTS_VOICE_A', 'Puck'), os.environ.get('GEMINI_TTS_VOICE_B', 'Kore')]
            if any(voice not in GEMINI_VOICES for voice in self.voices):
                raise ValueError('Select a supported Gemini prebuilt voice in GEMINI_TTS_VOICE_A/B.')
        if self.kind == 'piper-http':
            if urlparse(location).scheme not in ('http', 'https'):
                raise ValueError('TTS_BASE_URL must be an HTTP or HTTPS service URL.')
            self.voices = [os.environ.get('STUDIO_VOICE_A', 'en_US-lessac-medium'),
                           os.environ.get('STUDIO_VOICE_B', 'en_US-ryan-medium')]
            # Validate keys because older Piper services otherwise select a fallback voice.
            try:
                with urlopen(location + '/api/voices', timeout=20) as response:
                    catalog = json.loads(response.read(4 * 1024 * 1024))
                keys = {v.get('key') for v in catalog.get('voices', []) if isinstance(v, dict)}
                missing = [voice for voice in self.voices if voice not in keys]
                if missing:
                    raise RuntimeError('Piper voice keys are unavailable: ' + ', '.join(missing))
            except (OSError, ValueError) as exc:
                raise RuntimeError(f'Could not load the selected Piper voice catalog: {exc}') from exc
        if self.kind == 'espeak-library':
            class ID(ctypes.Union):
                _fields_ = [('number', ctypes.c_int), ('name', ctypes.c_char_p), ('string', ctypes.c_char * 8)]
            class Event(ctypes.Structure):
                _fields_ = [('type', ctypes.c_int), ('unique_identifier', ctypes.c_uint),
                            ('text_position', ctypes.c_int), ('length', ctypes.c_int),
                            ('audio_position', ctypes.c_int), ('sample', ctypes.c_int),
                            ('user_data', ctypes.c_void_p), ('id', ID)]
            self.lib = ctypes.CDLL(location)
            self.lib.espeak_Initialize.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_char_p, ctypes.c_int]
            self.lib.espeak_Initialize.restype = ctypes.c_int
            self.rate = self.lib.espeak_Initialize(2, 0, None, 0)  # AUDIO_OUTPUT_SYNCHRONOUS
            if self.rate <= 0:
                raise RuntimeError('eSpeak could not initialize. Install its voice data package.')
            callback_type = ctypes.CFUNCTYPE(ctypes.c_int, ctypes.POINTER(ctypes.c_short), ctypes.c_int,
                                             ctypes.POINTER(Event))
            def collect(samples, count, events):
                if _CANCELED:
                    return 1
                if samples and count:
                    self.chunks.append(ctypes.string_at(samples, count * 2))
                i = 0
                while events and events[i].type != 0:
                    event = events[i]
                    if event.type == 1:
                        self.words.append((event.text_position - 1, event.audio_position / 1000))
                    i += 1
                return 0
            self.callback = callback_type(collect)  # Keep callback alive across C calls.
            self.lib.espeak_SetSynthCallback.argtypes = [callback_type]
            self.lib.espeak_SetSynthCallback(self.callback)
            self.lib.espeak_SetVoiceByName.argtypes = [ctypes.c_char_p]
            self.lib.espeak_SetParameter.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int]
            self.lib.espeak_Synth.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_uint,
                                             ctypes.c_int, ctypes.c_uint, ctypes.c_uint,
                                             ctypes.POINTER(ctypes.c_uint), ctypes.c_void_p]

    def speak(self, text, speaker, workdir):
        if self.lib:
            self.chunks, self.words = [], []
            voice = b'en-us+f3' if speaker == 0 else b'en-us+m3'
            if self.lib.espeak_SetVoiceByName(voice) != 0:
                raise RuntimeError('eSpeak English voice data is missing.')
            self.lib.espeak_SetParameter(1, 163 if speaker == 0 else 156, 0)
            self.lib.espeak_SetParameter(3, 56 if speaker == 0 else 42, 0)
            data = text.encode('utf-8') + b'\0'
            buffer = ctypes.create_string_buffer(data)
            identifier = ctypes.c_uint()
            status = self.lib.espeak_Synth(buffer, len(data), 0, 1, 0, 1, ctypes.byref(identifier), None)
            self.lib.espeak_Synchronize()
            if _CANCELED:
                raise InterruptedError('Render canceled.')
            if status:
                raise RuntimeError(f'eSpeak synthesis failed (code {status}).')
            pcm = b''.join(self.chunks)
            words = list(self.words)
        elif self.kind == 'gemini-cloud':
            payload = {'contents': [{'parts': [{'text': text}]}],
                       'generationConfig': {'responseModalities': ['AUDIO'],
                                            'speechConfig': {'voiceConfig': {'prebuiltVoiceConfig': {'voiceName': self.voices[speaker]}}}}}
            request = Request('https://generativelanguage.googleapis.com/v1beta/models/' + self.model + ':generateContent',
                              data=json.dumps(payload).encode('utf-8'),
                              headers={'Content-Type': 'application/json', 'x-goog-api-key': self.command}, method='POST')
            try:
                with urlopen(request, timeout=90) as response:
                    data = response.read(48 * 1024 * 1024 + 1)
                if len(data) > 48 * 1024 * 1024:
                    raise RuntimeError('Gemini speech response exceeds the per-scene limit.')
                result = json.loads(data)
            except HTTPError as exc:
                # Never include provider response bodies or request headers, which could expose credentials.
                reason = 'quota or rate limit reached' if exc.code == 429 else 'authentication failed' if exc.code in (401, 403) else 'request rejected'
                raise RuntimeError(f'Gemini speech {reason} (HTTP {exc.code}); no local fallback was used.') from exc
            except TimeoutError as exc:
                raise RuntimeError('Gemini speech timed out; retry the job. No local fallback was used.') from exc
            except URLError as exc:
                if isinstance(exc.reason, TimeoutError):
                    raise RuntimeError('Gemini speech timed out; retry the job. No local fallback was used.') from exc
                raise RuntimeError('Gemini speech service could not be reached; no local fallback was used.') from exc
            candidates = result.get('candidates') or []
            if not candidates or not isinstance(candidates[0], dict):
                raise RuntimeError('Gemini returned no completed speech candidate; the request may have been blocked.')
            if candidates[0].get('finishReason') != 'STOP':
                raise RuntimeError('Gemini speech did not finish successfully; truncated or blocked audio was discarded. Retry with a shorter scene.')
            parts = candidates[0].get('content', {}).get('parts', [])
            chunks = []
            self.rate = 24000
            for part in parts:
                inline = part.get('inlineData') or part.get('inline_data')
                if not isinstance(inline, dict): continue
                mime = inline.get('mimeType') or inline.get('mime_type', '')
                if not mime.lower().startswith(('audio/l16', 'audio/pcm')):
                    raise RuntimeError('Gemini returned unsupported audio; expected 16-bit PCM.')
                match = re.search(r'rate=(\d+)', mime)
                rate = int(match.group(1)) if match else 24000
                if rate != 24000:
                    raise RuntimeError('Gemini returned an unexpected PCM sample rate.')
                try: chunks.append(base64.b64decode(inline['data'], validate=True))
                except (ValueError, KeyError) as exc: raise RuntimeError('Gemini returned invalid PCM data.') from exc
            pcm = b''.join(chunks)
            if not pcm or len(pcm) % 2:
                raise RuntimeError('Gemini returned no usable speech audio; the request may have been blocked.')
            words = []
        elif self.kind == 'piper-http':
            raw = Path(workdir) / 'piper-response.wav'
            target = Path(workdir) / 'piper-normalized.wav'
            payload = json.dumps({'text': text, 'voice_key': self.voices[speaker]}).encode('utf-8')
            request = Request(self.command + '/api/tts', data=payload,
                              headers={'Content-Type': 'application/json'}, method='POST')
            try:
                with urlopen(request, timeout=120) as response:
                    data = response.read(32 * 1024 * 1024 + 1)
                if len(data) > 32 * 1024 * 1024:
                    raise RuntimeError('Piper response exceeds the 32 MB per-scene limit.')
                raw.write_bytes(data)
            except HTTPError as exc:
                detail = exc.read(500).decode('utf-8', errors='replace')
                raise RuntimeError(f'Piper returned HTTP {exc.code}: {detail}') from exc
            except URLError as exc:
                raise RuntimeError(f'Piper speech request failed: {exc.reason}') from exc
            completed = subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y',
                                        '-threads', '1', '-i', str(raw), '-vn', '-ac', '1',
                                        '-ar', '22050', '-c:a', 'pcm_s16le', str(target)],
                                       capture_output=True, timeout=30, check=False)
            if completed.returncode:
                raise RuntimeError('Piper did not return decodable audio: ' + completed.stderr.decode(errors='replace')[-500:])
            with wave.open(str(target), 'rb') as audio:
                self.rate = audio.getframerate()
                pcm = audio.readframes(audio.getnframes())
            words = []
        else:
            target = Path(workdir) / 'speech.wav'
            completed = subprocess.run([self.command, '-v', 'en-us+f3' if speaker == 0 else 'en-us+m3',
                                        '-s', '163' if speaker == 0 else '156', '-w', str(target), '--stdin'],
                                       input=text, text=True, capture_output=True, timeout=60, check=False)
            if completed.returncode:
                raise RuntimeError('eSpeak narration failed: ' + completed.stderr[-500:])
            with wave.open(str(target), 'rb') as audio:
                if audio.getnchannels() != 1 or audio.getsampwidth() != 2:
                    raise RuntimeError('eSpeak returned an unsupported audio format.')
                self.rate = audio.getframerate()
                pcm = audio.readframes(audio.getnframes())
            words = []
        samples = array.array('h', pcm)
        if not samples or max(abs(v) for v in samples) < 10:
            raise RuntimeError('Speech synthesis returned empty or silent audio; no video was created.')
        return pcm, self.rate, words

    def close(self):
        if self.lib:
            self.lib.espeak_Terminate()
            self.lib = None


def validate(request):
    if not isinstance(request, dict) or not isinstance(request.get('draft'), dict):
        raise ValueError('Request must contain a reviewed draft object.')
    draft = request['draft']
    quality = request.get('quality', 'preview')
    if quality not in ('preview', 'full'): raise ValueError('Quality must be preview or full.')
    if draft.get('format', 'portrait') not in FORMATS: raise ValueError('Unsupported video format.')
    pair = draft.get('pair', 'cog-axiom')
    studio_cast.get_pair(pair)
    if request.get('voice_provider') not in (None, '', 'espeak', 'gemini', 'piper'):
        raise ValueError('Speech provider must be gemini, espeak, or piper.')
    scenes = draft.get('scenes')
    if not isinstance(scenes, list) or not 1 <= len(scenes) <= 24:
        raise ValueError('A video must contain between 1 and 24 scenes.')
    total = 0
    for scene in scenes:
        if not isinstance(scene, dict) or type(scene.get('speaker')) is not int or scene['speaker'] not in (0, 1):
            raise ValueError('Each scene needs a speaker index of 0 or 1.')
        if not isinstance(scene.get('text'), str) or not scene['text'].strip():
            raise ValueError('Each scene needs nonempty spoken text.')
        if len(scene['text']) > 2400: raise ValueError('Keep each scene under 2400 characters.')
        if '\x00' in scene['text']: raise ValueError('Spoken text cannot contain null bytes.')
        if not isinstance(scene.get('visual', ''), str): raise ValueError('Scene visual must be text.')
        total += len(scene['text'])
    if total > 20000: raise ValueError('Keep the video script under 20000 characters.')
    for field in ('title', 'topic'):
        if field in draft and not isinstance(draft[field], str): raise ValueError(f'Draft {field} must be text.')
    return draft, quality


def timestamp(seconds):
    millis = max(0, round(seconds * 1000))
    hours, millis = divmod(millis, 3600000)
    minutes, millis = divmod(millis, 60000)
    secs, millis = divmod(millis, 1000)
    return f'{hours:02}:{minutes:02}:{secs:02},{millis:03}'


def timed_words(text, events, duration, lead=0.12):
    matches = list(re.finditer(r'\S+', text))
    starts = []
    event_positions = [pos for pos, _ in events]
    for idx, match in enumerate(matches):
        if events:
            event_idx = max(0, bisect.bisect_right(event_positions, match.start()) - 1)
            start = events[event_idx][1] + lead
        else:
            start = lead + (duration - lead - .2) * match.start() / max(len(text), 1)
        # Some punctuation/compound tokens share events. Give them ordered timings.
        starts.append(min(duration - .02, max(start, starts[-1] + .025 if starts else lead)))
    return [{'text': match.group(), 'start': starts[idx],
             'end': starts[idx+1] if idx+1 < len(starts) else duration - .08}
            for idx, match in enumerate(matches)]


def font_path(bold=False):
    override = os.environ.get('STUDIO_FONT_BOLD' if bold else 'STUDIO_FONT')
    options = [override] if override else []
    options += [str(ROOT / 'assets' / 'fonts' / ('DejaVuSans-Bold.ttf' if bold else 'DejaVuSans.ttf')),
                '/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf' if bold else '/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf',
                '/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf' if bold else '/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf',
                '/Library/Fonts/Arial Bold.ttf' if bold else '/Library/Fonts/Arial.ttf',
                'C:/Windows/Fonts/arialbd.ttf' if bold else 'C:/Windows/Fonts/arial.ttf']
    return next((p for p in options if p and Path(p).is_file()), None)


class Design:
    def __init__(self, draft, size):
        from PIL import Image, ImageDraw, ImageFont, ImageOps
        self.Image, self.Draw, self.Font, self.Ops = Image, ImageDraw, ImageFont, ImageOps
        self.w, self.h = size
        self.scale = self.w / 1080
        self.portrait = self.h > self.w
        self.landscape = self.w > self.h
        self.draft = draft
        self.fonts = {}
        pair = studio_cast.get_pair(draft.get('pair', 'cog-axiom'))
        self.name = pair['name']
        self.speakers = [s['name'] for s in pair['speakers']]
        self.avatars = []
        avatar_w = self.w * (.235 if self.landscape else .40 if self.portrait else .28)
        avatar_h = self.h * (.60 if self.landscape else .29 if self.portrait else .36)
        for speaker in pair['speakers']:
            expressions = {}
            for expression in ('idle', 'talk', 'blink'):
                path = Path(speaker.get(expression, speaker['idle']))
                if not path.is_file(): raise ValueError(f'Missing presenter artwork for {speaker["name"]}.')
                with Image.open(path) as original:
                    if original.width > 4096 or original.height > 4096 or original.width * original.height > 20000000:
                        raise ValueError('Presenter image is too large; resize it below 20 megapixels.')
                    original.load()
                    rgba = original.convert('RGBA')
                    box = rgba.getbbox()
                    if box: rgba = rgba.crop(box)
                    expressions[expression] = ImageOps.contain(rgba, (int(avatar_w), int(avatar_h)), Image.Resampling.LANCZOS)
            self.avatars.append(expressions)
        self.captions = {}

    def font(self, size, bold=False):
        # Sizes are expressed relative to the shortest output edge.
        pixels = max(10, round(size * min(self.w, self.h) / 1080))
        key = (pixels, bold)
        if key not in self.fonts:
            path = font_path(bold)
            self.fonts[key] = self.Font.truetype(path, pixels) if path else self.Font.load_default(size=pixels)
        return self.fonts[key]

    def wrap(self, text, font, width, lines=8):
        d = self.Draw.Draw(self.Image.new('RGB', (1, 1)))
        result, current = [], ''
        for word in text.split():
            # Split unusually long source tokens so imported scripts cannot overflow.
            fragments = []
            while d.textlength(word, font=font) > width and len(word) > 1:
                cut = len(word) - 1
                while cut > 1 and d.textlength(word[:cut], font=font) > width: cut -= 1
                fragments.append(word[:cut]); word = word[cut:]
            fragments.append(word)
            for piece in fragments:
                candidate = (current + ' ' + piece).strip()
                if current and d.textlength(candidate, font=font) > width:
                    result.append(current); current = piece
                else: current = candidate
        if current: result.append(current)
        if len(result) > lines:
            result = result[:lines]
            tail = result[-1]
            while tail and d.textlength(tail + '…', font=font) > width: tail = tail[:-1]
            result[-1] = tail.rstrip() + '…'
        return result

    def block(self, draw, text, xy, width, size, color=None, bold=False, lines=8, spacing=1.3):
        font = self.font(size, bold)
        line_height = round(font.size * spacing)
        for i, line in enumerate(self.wrap(text, font, width, lines)):
            draw.text((xy[0], xy[1] + i * line_height), line, font=font, fill=color or COLORS['ink'])
        return line_height

    def base(self, scene, index, count):
        im = self.Image.new('RGB', (self.w, self.h), COLORS['bg']); d = self.Draw.Draw(im)
        w, h = self.w, self.h; u = min(w,h)/1080; margin=56*u
        # Quiet editorial grid and top label.
        for x in range(0,w,int(72*u)):
            for y in range(0,h,int(72*u)):
                d.ellipse((x,y,x+1,y+1),fill='#DCE0D6')
        d.ellipse((margin,margin+6*u,margin+18*u,margin+24*u), fill=COLORS['coral'])
        d.text((margin+30*u,margin), 'CONTENT FARM  /  FIELD NOTES', font=self.font(21,True),fill=COLORS['teal'])
        label=f'{index+1:02} / {count:02}'
        d.text((w-margin-d.textlength(label,font=self.font(24,True)),margin),label,font=self.font(24,True),fill=COLORS['muted'])
        title = self.draft.get('title') or self.draft.get('topic') or 'A new perspective'
        if self.portrait:
            self.block(d,title,(margin,122*u),w-2*margin,67,bold=True,lines=3,spacing=1.14)
            card=(margin,420*u,w-margin,958*u)
            self.avatar_centers=(w*.28,w*.72);self.avatar_bottom=h*.79
            self.caption_box=(margin,h*.84,w-margin,h*.963)
        elif self.landscape:
            self.block(d,title,(margin,123*u),w*.68,58,bold=True,lines=2,spacing=1.16)
            card=(w*.30,h*.32,w*.70,h*.70)
            self.avatar_centers=(w*.15,w*.85);self.avatar_bottom=h*.83
            self.caption_box=(w*.22,h*.80,w*.78,h*.955)
        else:
            self.block(d,title,(margin,115*u),w-2*margin,47,bold=True,lines=2,spacing=1.16)
            card=(margin,h*.28,w-margin,h*.49)
            self.avatar_centers=(w*.26,w*.74);self.avatar_bottom=h*.87
            self.caption_box=(w*.36,h*.55,w*.64,h*.86)
        self.card = card
        radius=26*u
        d.rounded_rectangle(card,radius,fill=COLORS['paper'],outline=COLORS['line'],width=max(1,int(2*u)))
        x0,y0,x1,y1=card
        d.rounded_rectangle((x0+28*u,y0+28*u,x0+109*u,y0+65*u),12*u,fill=COLORS['mint'])
        d.text((x0+42*u,y0+35*u),'IDEA',font=self.font(16,True),fill=COLORS['teal'])
        visual = scene.get('visual','').strip() or 'One clear idea. Two different perspectives.'
        if self.portrait:
            self.block(d,visual,(x0+34*u,y0+95*u),x1-x0-68*u,39,bold=True,lines=5,spacing=1.3)
            self.diagram(d,(x0+65*u,y1-135*u,x1-65*u,y1-50*u),index)
        elif self.landscape:
            self.block(d,visual,(x0+30*u,y0+86*u),x1-x0-60*u,31,bold=True,lines=6,spacing=1.26)
        else:
            self.block(d,visual,(x0+145*u,y0+30*u),x1-x0-182*u,31,bold=True,lines=4,spacing=1.2)
        for i, center in enumerate(self.avatar_centers):
            bw = (235 if self.landscape else 258 if self.portrait else 210)*u
            by=self.avatar_bottom+18*u
            active=i==scene['speaker']
            d.rounded_rectangle((center-bw/2,by,center+bw/2,by+47*u),20*u,
                                fill=COLORS['teal'] if active else COLORS['mint'])
            font=self.font(24,True);text=self.speakers[i]
            d.text((center-d.textlength(text,font=font)/2,by+8*u),text,font=font,
                   fill=COLORS['paper'] if active else COLORS['teal'])
        d.text((margin,h-26*u),'A CONVERSATION WORTH SHARING',font=self.font(12,True),fill=COLORS['muted'])
        return im

    def diagram(self, d, box, index):
        x0,y0,x1,y1=box;mid=(y0+y1)/2;radius=(y1-y0)*.36
        points=(x0+radius,(x0+x1)/2,x1-radius)
        d.line((points[0],mid,points[-1],mid),fill='#B2CAC1',width=max(2,round(4*min(self.w,self.h)/1080)))
        for i,x in enumerate(points):
            fill=COLORS['coral'] if i==index%3 else COLORS['mint']
            d.ellipse((x-radius,mid-radius,x+radius,mid+radius),fill=fill)
            if i==0: d.rectangle((x-radius*.3,mid-radius*.3,x+radius*.3,mid+radius*.3),outline=COLORS['teal'],width=2)
            elif i==1: d.ellipse((x-radius*.3,mid-radius*.3,x+radius*.3,mid+radius*.3),outline=COLORS['teal'],width=2)
            else: d.polygon(((x,mid-radius*.4),(x+radius*.4,mid+radius*.3),(x-radius*.4,mid+radius*.3)),fill=COLORS['teal'])

    def caption_groups(self, words):
        x0,y0,x1,y1=self.caption_box
        width=x1-x0-38*min(self.w,self.h)/1080
        font=self.font(35 if self.portrait else 31,True)
        groups=[];current=[]
        for word in words:
            proposed=current+[word]
            lines=self.wrap(' '.join(w['text'] for w in proposed),font,width,99)
            if current and (len(lines)> (5 if not self.portrait and not self.landscape else 3) or len(proposed)>8):
                groups.append(current);current=[word]
            else: current=proposed
        if current:groups.append(current)
        return groups

    def frame(self, base, scene, words, groups, t, energy, global_progress):
        frame=base.copy();d=self.Draw.Draw(frame);u=min(self.w,self.h)/1080
        for i, expressions in enumerate(self.avatars):
            active=i==scene['speaker']; talking=active and energy>.038
            blinking=(int((t+i*1.3)*18)%79 in (0,1))
            expression='blink' if blinking else 'talk' if talking else 'idle'
            avatar=expressions[expression]
            bob=math.sin(t*11)*min(9,energy*28)*u if active else math.sin(t*1.5+i)*2*u
            xy=(round(self.avatar_centers[i]-avatar.width/2),round(self.avatar_bottom-avatar.height-bob))
            frame.paste(avatar,xy,avatar)
        x0,y0,x1,y1=self.caption_box
        d.rounded_rectangle((x0,y0,x1,y1),20*u,fill=COLORS['ink'])
        current_idx=max(0,bisect.bisect_right([w['start'] for w in words],t)-1)
        current=words[current_idx]
        group=next((g for g in groups if g[0]['start']<=current['start']<=g[-1]['start']),groups[0])
        font=self.font(35 if self.portrait else 31,True)
        lines=self.wrap(' '.join(w['text'] for w in group),font,x1-x0-38*u,5 if not self.portrait and not self.landscape else 3)
        line_h=font.size*1.38
        text_y=(y0+y1-len(lines)*line_h)/2-1*u
        token_idx=0
        for line in lines:
            text_x=(x0+x1-d.textlength(line,font=font))/2
            for token in line.split():
                span=d.textlength(token,font=font)
                # Match by chronological token index, so repeated words highlight correctly.
                hot=token_idx<len(group) and group[token_idx] is current
                if hot:d.rounded_rectangle((text_x-4*u,text_y-1*u,text_x+span+4*u,text_y+line_h-3*u),5*u,fill=COLORS['coral'])
                d.text((text_x,text_y),token,font=font,fill=COLORS['paper'])
                text_x+=span+d.textlength(' ',font=font);token_idx+=1
            text_y+=line_h
        # Listening bars are driven by the actual speech envelope.
        center=self.avatar_centers[scene['speaker']]
        bar_y=self.avatar_bottom+82*u
        for b in range(13):
            height=(4+energy*20*(.45+.55*math.sin(t*13+b*.8)**2))*u
            x=center+(b-6)*9*u
            d.rounded_rectangle((x,bar_y-height/2,x+4*u,bar_y+height/2),2*u,fill=COLORS['teal'])
        d.rectangle((0,self.h-5*u,self.w*global_progress,self.h),fill=COLORS['coral'])
        return frame


def stop_process():
    global _ACTIVE_PROCESS
    process=_ACTIVE_PROCESS
    _ACTIVE_PROCESS=None
    if process and process.poll() is None:
        process.terminate()
        try:process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill();process.wait(timeout=3)


def interrupted(signum, frame):
    global _CANCELED
    _CANCELED = True
    stop_process()
    raise InterruptedError('Render canceled.')


def render(request, output):
    global _ACTIVE_PROCESS, _CANCELED
    _CANCELED = False
    draft,quality=validate(request)
    provider=request.get('voice_provider') or default_voice_provider()
    cap=capabilities(provider)
    if not cap['ready']: raise RuntimeError(cap['reason'])
    if not next(p for p in cap['pairs'] if p['id']==draft.get('pair','cog-axiom'))['available']:
        raise ValueError('This personal cast is not installed. Add your own PNGs or choose Nova & Atlas.')
    width,height,fps,resource_mode=resource_settings(quality,draft.get('format','portrait'))
    low_priority()
    cool_down(1)
    output=Path(output).resolve()
    if output.suffix.lower()!='.mp4':raise ValueError('Output must end in .mp4.')
    output.parent.mkdir(parents=True,exist_ok=True)
    scenes=draft['scenes'];speech=Speech(provider);prepared=[];elapsed=0;pcm_parts=[];all_captions=[]
    started=time.monotonic()
    with tempfile.TemporaryDirectory(prefix='.studio-render-',dir=output.parent) as temp:
        try:
            voice_label = 'Gemini cloud' if speech.kind == 'gemini-cloud' else 'Piper' if speech.kind == 'piper-http' else 'lightweight eSpeak'
            progress('narration',3,f'Creating {voice_label} narration for {len(scenes)} scene{"s" if len(scenes) != 1 else ""}')
            for index,scene in enumerate(scenes):
                cool_down(3+22*index/len(scenes))
                pcm,rate,events=speech.speak(scene['text'].strip(),scene['speaker'],temp)
                if prepared and rate!=prepared[0]['rate']:raise RuntimeError('Speech sample rate changed between scenes.')
                lead=b'\0\0'*round(rate*.12);tail=b'\0\0'*round(rate*.22)
                pcm=lead+pcm+tail
                frames=math.ceil(len(pcm)/(2*rate)*fps)
                length=frames/fps
                if elapsed+length>600:raise ValueError('Narration exceeds the 10 minute render limit. Shorten the script.')
                pcm=pcm.ljust(round(length*rate)*2,b'\0')
                words=timed_words(scene['text'].strip(),events,length)
                samples=array.array('h',pcm)
                envelope=[]
                for f in range(frames):
                    block=samples[int(f*rate/fps):int((f+1)*rate/fps)]
                    rms=math.sqrt(sum(v*v for v in block)/max(1,len(block)))/32768
                    envelope.append(min(1,rms*8))
                prepared.append({'scene':scene,'words':words,'duration':length,'frames':frames,
                                 'energy':envelope,'start':elapsed,'rate':rate,'word_events':bool(events)})
                pcm_parts.append(pcm);elapsed+=length
                progress('narration',3+22*(index+1)/len(scenes),f'Narrated scene {index+1} of {len(scenes)}')
            speech.close()
            audio_path=Path(temp)/'narration.wav'
            with wave.open(str(audio_path),'wb') as audio:
                audio.setnchannels(1);audio.setsampwidth(2);audio.setframerate(rate);audio.writeframes(b''.join(pcm_parts))
            del pcm_parts
            design=Design(draft,(width,height))
            movie_path=Path(temp)/'video.mp4';poster_path=Path(temp)/'video.jpg'
            log_path=Path(temp)/'ffmpeg.log'
            total_frames=sum(s['frames'] for s in prepared)
            command=['ffmpeg','-hide_banner','-loglevel','error','-y','-filter_threads','1',
                     '-f','rawvideo','-pix_fmt','rgb24','-s',f'{width}x{height}','-r',str(fps),'-i','pipe:0',
                     '-i',str(audio_path),'-map','0:v:0','-map','1:a:0','-c:v','libx264','-threads','1',
                     '-preset','veryfast','-crf','23' if quality=='preview' else '20','-pix_fmt','yuv420p',
                     '-c:a','aac','-b:a','128k','-movflags','+faststart','-shortest',str(movie_path)]
            with log_path.open('wb') as log:
                _ACTIVE_PROCESS=subprocess.Popen(command,stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=log)
                process=_ACTIVE_PROCESS;written=0;last_report=0;last_thermal=time.monotonic()
                try:
                    for index,item in enumerate(prepared):
                        cool_down(25+68*written/total_frames)
                        base=design.base(item['scene'],index,len(prepared))
                        groups=design.caption_groups(item['words'])
                        for group in groups:
                            all_captions.append((item['start']+group[0]['start'],item['start']+group[-1]['end'],
                                                 ' '.join(w['text'] for w in group)))
                        for f in range(item['frames']):
                            frame=design.frame(base,item['scene'],item['words'],groups,f/fps,item['energy'][f],written/total_frames)
                            if index==0 and f==min(fps,item['frames']-1):frame.save(poster_path,quality=93)
                            process.stdin.write(frame.tobytes());written+=1
                            now=time.monotonic()
                            if now-last_thermal >= 5:
                                cool_down(25+68*written/total_frames)
                                last_thermal=time.monotonic()
                            if now-last_report>1:
                                progress('rendering',25+68*written/total_frames,f'Animating scene {index+1} of {len(prepared)}')
                                last_report=now
                    process.stdin.close()
                    code=process.wait(timeout=120)
                except BrokenPipeError:
                    process.wait(timeout=10)
                    raise RuntimeError('FFmpeg encoding failed: '+log_path.read_text(errors='replace')[-1200:])
                if code:raise RuntimeError('FFmpeg encoding failed: '+log_path.read_text(errors='replace')[-1200:])
                _ACTIVE_PROCESS=None
            progress('finalizing',96,'Verifying the video and writing captions')
            if not movie_path.is_file() or movie_path.stat().st_size<1000:raise RuntimeError('FFmpeg produced no usable video.')
            if shutil.which('ffprobe'):
                probe=subprocess.run(['ffprobe','-v','error','-show_entries','stream=codec_type,width,height:format=duration',
                                      '-of','json',str(movie_path)],capture_output=True,text=True,timeout=15,check=True)
                media=json.loads(probe.stdout)
                if {s['codec_type'] for s in media['streams']}!={'audio','video'}:
                    raise RuntimeError('Output verification failed: audio or video stream missing.')
            srt='\n\n'.join(f'{i+1}\n{timestamp(start)} --> {timestamp(end)}\n{text}'
                            for i,(start,end,text) in enumerate(all_captions))+'\n'
            captions_path=Path(temp)/'video.srt';captions_path.write_text(srt,encoding='utf-8')
            metadata={'title':draft.get('title',''), 'topic':draft.get('topic',''), 'pair':draft.get('pair','cog-axiom'),
                      'format':draft.get('format','portrait'),'quality':quality,'width':width,'height':height,'fps':fps,
                      'duration_seconds':round(elapsed,3),'voice_engine':cap['voice_engine'],'scene_count':len(scenes),
                      'presenter_animation':next(p['presenter_animation'] for p in cap['pairs'] if p['id']==draft.get('pair','cog-axiom')),
                      'voice_provider':provider,'resource_mode':resource_mode,'encoder_threads':1,
                      'render_seconds':round(time.monotonic()-started,2),
                      'caption_timing':'speech-word-events' if all(s['word_events'] for s in prepared) else 'estimated-word-timing',
                      'scenes':[{'speaker':s['scene']['speaker'],'start':round(s['start'],3),'duration':round(s['duration'],3)} for s in prepared]}
            metadata_path=Path(temp)/'video.json';metadata_path.write_text(json.dumps(metadata,indent=2)+'\n',encoding='utf-8')
            # Publish only after every deliverable exists and the encoder has succeeded.
            for source,suffix in ((poster_path,'.jpg'),(captions_path,'.srt'),(metadata_path,'.json'),(movie_path,'.mp4')):
                os.replace(source,output.with_suffix(suffix))
            progress('completed',100,f'Rendered {elapsed:.1f}s with narrated audio and captions')
            return metadata
        finally:
            speech.close();stop_process()


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input',type=Path,help='Path to a reviewed render-request JSON file')
    parser.add_argument('--output',type=Path,help='Destination MP4 file')
    parser.add_argument('--capabilities','--check',action='store_true',help='Print local renderer readiness as JSON')
    args=parser.parse_args(argv)
    if args.capabilities:
        print(json.dumps(capabilities()));return 0
    if not args.input or not args.output:parser.error('--input and --output are required')
    signal.signal(signal.SIGTERM,interrupted);signal.signal(signal.SIGINT,interrupted)
    try:
        if args.input.stat().st_size>1024*1024:raise ValueError('Render request exceeds 1 MB.')
        request=json.loads(args.input.read_text(encoding='utf-8'))
        render(request,args.output)
    except (Exception,KeyboardInterrupt) as exc:
        stop_process()
        print(f'Render failed: {exc}',file=sys.stderr,flush=True)
        return 1
    return 0


if __name__=='__main__':
    raise SystemExit(main())
