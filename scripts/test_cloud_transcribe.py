"""Cloud boundary, caption timing and resumable chunk ownership tests."""
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import cloud_transcribe as cloud
import shorts


class CloudTranscriptionTests(unittest.TestCase):
    def test_rejects_invalid_timestamps_and_words(self):
        invalid = [
            {'word': 'hello', 'start': True, 'end': 1},
            {'word': 'hello', 'start': 0, 'end': float('nan')},
            {'word': 'hello', 'start': -1, 'end': 1},
            {'word': 'hello', 'start': 2, 'end': 1},
            {'word': 'hello', 'start': 0, 'end': 50},
            {'word': '', 'start': 0, 'end': 1},
        ]
        for word in invalid:
            with self.subTest(word=word), self.assertRaises(ValueError):
                cloud.validate_transcript({'language': 'en', 'words': [word]}, 4)
        with self.assertRaises(ValueError):
            cloud.validate_transcript({'words': [{'word': 'a', 'start': 2, 'end': 3},
                                                {'word': 'b', 'start': 1, 'end': 2}]}, 4)
        self.assertEqual(cloud.validate_transcript({'language': 'en', 'words': []}, 4)['words'], [])

    def test_cloud_request_uses_header_and_structured_response(self):
        result = {'language': 'EN', 'words': [{'word': 'Hello.', 'start': 0.1, 'end': 0.8}]}
        envelope = {'candidates': [{'finishReason': 'STOP', 'content': {'parts': [{'text': json.dumps(result)}]}}]}
        with tempfile.TemporaryDirectory() as directory:
            audio = Path(directory) / 'audio.wav'
            audio.write_bytes(b'fixture audio')
            with patch.dict(os.environ, {'GEMINI_API_KEY': 'test-secret'}, clear=True), \
                 patch.object(cloud, 'urlopen', return_value=io.BytesIO(json.dumps(envelope).encode())) as send:
                actual = cloud.transcribe_audio(audio, 1)
            request = send.call_args.args[0]
            payload = json.loads(request.data)
            self.assertNotIn('test-secret', request.full_url)
            self.assertEqual(request.get_header('X-goog-api-key'), 'test-secret')
            self.assertEqual(payload['generationConfig']['responseMimeType'], 'application/json')
            self.assertEqual(actual['language'], 'en')
            self.assertEqual(actual['words'], result['words'])

    def test_provider_errors_never_leak_response_or_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            audio = Path(directory) / 'audio.wav'
            audio.write_bytes(b'fixture')
            error = HTTPError('https://example.com', 429, 'private provider response', {}, None)
            with patch.dict(os.environ, {'GEMINI_API_KEY': 'test-secret'}, clear=True), \
                 patch.object(cloud, 'urlopen', side_effect=error):
                with self.assertRaisesRegex(RuntimeError, 'HTTP 429') as caught:
                    cloud.transcribe_audio(audio, 1)
                self.assertNotIn('test-secret', str(caught.exception))
                self.assertNotIn('private provider response', str(caught.exception))

    def test_local_models_rejected_before_loading_or_inspecting_source(self):
        with patch.dict(os.environ, {'SHORTS_TRANSCRIBER': 'cpu', 'ALLOW_LOCAL_MODELS': 'false'}):
            with self.assertRaisesRegex(RuntimeError, 'Local transcription models are disabled'):
                shorts.transcribe(Path('/nonexistent'), Path('/nonexistent'), '', 'tiny')

    def test_chunk_boundaries_are_owned_once_and_cached_for_resume(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            source = work / 'source.mp4'
            source.write_bytes(b'fixture source')
            responses = [
                {'language': 'en', 'words': [{'word': 'first', 'start': 1, 'end': 2},
                                           {'word': 'boundary', 'start': 60.1, 'end': 60.5}]},
                {'language': 'en', 'words': [{'word': 'previous', 'start': 1, 'end': 1.5},
                                           {'word': 'boundary', 'start': 2.1, 'end': 2.5},
                                           {'word': 'last', 'start': 61, 'end': 61.5}]},
            ]
            with patch.dict(os.environ, {'GEMINI_API_KEY': 'test-secret'}), \
                 patch.object(shorts, 'probe', return_value={'format': {'duration': 120}}), \
                 patch.object(shorts, 'run'), patch.object(shorts, 'cool_down'), \
                 patch.object(shorts, 'require_space'), patch.object(shorts, 'emit'), \
                 patch.object(cloud, 'transcribe_audio', side_effect=responses) as transcribe:
                result = shorts.transcribe_cloud(source, work, '')
                self.assertEqual([word['word'] for word in result['words']], ['first', 'boundary', 'last'])
                self.assertEqual(result['timing'], 'model-estimated')
                self.assertEqual(transcribe.call_count, 2)
                (work / 'transcript.json').unlink()
                resumed = shorts.transcribe_cloud(source, work, '')
                self.assertEqual(resumed, result)
                self.assertEqual(transcribe.call_count, 2)


if __name__ == '__main__':
    unittest.main()
