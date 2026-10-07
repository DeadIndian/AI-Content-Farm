#!/usr/bin/env python3
"""Meaningful renderer checks: speech, decoded media, formats, provider errors, cleanup."""
import array
import base64
from contextlib import redirect_stdout
import io
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import wave
from urllib.request import Request
from urllib.error import URLError

import studio_render as renderer
import studio_cast


def request(format='portrait', quality='preview'):
    return {'voice_provider': 'espeak', 'draft': {'title': 'Ideas become clearer', 'topic': 'Clear thinking', 'pair': 'cog-axiom',
                      'format': format, 'provider': 'manual',
                      'scenes': [{'speaker': 0, 'text': 'Make one clear point.',
                                  'visual': 'One idea, explained clearly.'},
                                 {'speaker': 1, 'text': 'Then test your understanding.',
                                  'visual': 'Can you explain it in your own words?'}]}, 'quality': quality}


class ValidationTests(unittest.TestCase):
    def test_rejects_invalid_requests(self):
        for change in ('empty', 'speaker', 'huge', 'pair', 'format', 'null', 'quality'):
            with self.subTest(change=change):
                data=request()
                if change=='empty':data['draft']['scenes']=[]
                if change=='speaker':data['draft']['scenes'][0]['speaker']=True
                if change=='huge':data['draft']['scenes'][0]['text']='a'*2401
                if change=='pair':data['draft']['pair']='../../bad'
                if change=='format':data['draft']['format']='cinema'
                if change=='null':data['draft']['scenes'][0]['text']='hello\0world'
                if change=='quality':data['quality']='unknown'
                with self.assertRaises(ValueError):renderer.validate(data)

    def test_word_event_captions_are_monotonic(self):
        words=renderer.timed_words('One clear idea.',[(0,0),(4,.2),(10,.7)],1.5)
        self.assertEqual([w['text'] for w in words],['One','clear','idea.'])
        self.assertAlmostEqual(words[1]['start'],.32)
        for word in words:self.assertLess(word['start'],word['end'])
        self.assertEqual(renderer.timestamp(3661.042),'01:01:01,042')

    def test_missing_personal_cast_fails_without_fallback(self):
        data=request();data['draft']['pair']='ryusui-senku'
        with tempfile.TemporaryDirectory() as tmp:
            with patch.object(studio_cast,'ASSETS',Path(tmp)):
                self.assertFalse(renderer.capabilities()['ready'])

    def test_personal_stills_do_not_claim_mouth_animation(self):
        with tempfile.TemporaryDirectory() as tmp:
            assets=Path(tmp);personal=assets/'personal';personal.mkdir()
            for name in ('ryusui','senku'):
                (personal/f'{name}-idle.png').write_bytes(b'still fixture')
            with patch.object(studio_cast,'ASSETS',assets):
                pair=next(p for p in renderer.capabilities()['pairs'] if p['id']=='ryusui-senku')
                self.assertTrue(pair['available'])
                self.assertEqual(set(pair['presenter_animation'].values()),{'audio-reactive-still'})
                (personal/'ryusui-talk.png').write_bytes(b'still fixture')
                self.assertEqual(studio_cast.get_pair('ryusui-senku')['presenter_animation']['Ryusui'],'audio-reactive-still')
                (personal/'ryusui-talk.png').write_bytes(b'distinct talking pose')
                self.assertEqual(studio_cast.get_pair('ryusui-senku')['presenter_animation']['Ryusui'],'mouth-poses-and-audio-motion')

    def test_gentle_defaults_and_cooling_hysteresis(self):
        with patch.dict(os.environ,{'STUDIO_RESOURCE_MODE':'gentle','STUDIO_PAUSE_TEMP_C':'75','STUDIO_RESUME_TEMP_C':'70'}):
            self.assertEqual(renderer.resource_settings('preview','portrait'),(360,640,12,'gentle'))
            self.assertEqual(renderer.resource_settings('full','landscape'),(1920,1080,24,'gentle'))
            with patch.object(renderer,'temperature',side_effect=[80,74,69]),patch.object(renderer.time,'sleep') as sleep,patch.object(renderer,'progress'):
                renderer.cool_down(20)
                self.assertEqual(sleep.call_count,2)
        with patch.dict(os.environ,{'STUDIO_PAUSE_TEMP_C':'0'}):
            with self.assertRaises(ValueError):renderer.thermal_thresholds()

    def test_piper_cannot_load_without_explicit_local_model_opt_in(self):
        with patch.dict(os.environ,{'ALLOW_LOCAL_MODELS':'false'}),patch.object(renderer,'urlopen') as network:
            self.assertEqual(renderer.voice_engine('piper'),('piper-blocked',None))
            with self.assertRaisesRegex(RuntimeError,'disabled'):renderer.Speech('piper')
            self.assertFalse(renderer.piper_readiness()[0])
            network.assert_not_called()

    def test_piper_is_available_independently_of_default_cloud_provider(self):
        def healthy(url,timeout):
            self.assertEqual(url,'http://piper.test/healthz')
            self.assertEqual(timeout,2)
            response=io.BytesIO(b'{}');response.status=200
            return response
        env={'ALLOW_LOCAL_MODELS':'true','TTS_BASE_URL':'http://piper.test',
             'STUDIO_TTS_PROVIDER':'gemini','GEMINI_API_KEY':'test-key-never-real'}
        with patch.dict(os.environ,env),patch.object(renderer,'urlopen',side_effect=healthy) as network:
            cap=renderer.capabilities()
            piper=next(provider for provider in cap['voice_providers'] if provider['id']=='piper')
            self.assertTrue(piper['available']);self.assertEqual(piper['reason'],'')
            self.assertEqual(cap['default_voice_provider'],'gemini')
            self.assertEqual(network.call_count,1)
            self.assertNotIn('test-key-never-real',json.dumps(cap))
            with patch.dict(os.environ,{'ALLOW_LOCAL_MODELS':'false'}):
                network.reset_mock()
                cap=renderer.capabilities()
                self.assertFalse(next(provider for provider in cap['voice_providers'] if provider['id']=='piper')['available'])
                network.assert_not_called()
        with patch.dict(os.environ,env),patch.object(renderer,'urlopen',side_effect=TimeoutError('private-transport-data')):
            available,reason=renderer.piper_readiness()
            self.assertFalse(available);self.assertNotIn('private-transport-data',reason)

    def test_gemini_cloud_pcm_contract_without_network_or_models(self):
        captured=[]
        samples=array.array('h',[0,200,-200,400,-400]*100).tobytes()
        def fake_open(request,timeout):
            captured.append(request)
            return io.BytesIO(json.dumps({'candidates':[{'finishReason':'STOP','content':{'parts':[{'inlineData':{'mimeType':'audio/L16;codec=pcm;rate=24000','data':base64.b64encode(samples).decode()}}]}}]}).encode())
        with patch.dict(os.environ,{'GEMINI_API_KEY':'test-key-never-real','GEMINI_TTS_MODEL':'gemini-2.5-flash-preview-tts'}),patch.object(renderer,'urlopen',side_effect=fake_open):
            speech=renderer.Speech('gemini')
            pcm,rate,events=speech.speak('A clear explanation.',0,'/unused')
            speech.close()
        self.assertEqual(pcm,samples);self.assertEqual(rate,24000);self.assertEqual(events,[])
        payload=json.loads(captured[0].data)
        self.assertEqual(payload['generationConfig']['speechConfig']['voiceConfig']['prebuiltVoiceConfig']['voiceName'],'Puck')
        self.assertNotIn('test-key',captured[0].full_url)

    def test_gemini_discards_incomplete_audio_and_reports_timeouts_without_secrets(self):
        samples=array.array('h',[200,-200]*20).tobytes()
        env={'GEMINI_API_KEY':'test-private-key','GEMINI_TTS_MODEL':'gemini-2.5-flash-preview-tts'}
        for finish in ('MAX_TOKENS','SAFETY','RECITATION',None):
            with self.subTest(finish=finish):
                candidate={'content':{'parts':[{'inlineData':{'mimeType':'audio/L16;rate=24000',
                            'data':base64.b64encode(samples).decode()}}]}}
                if finish is not None:candidate['finishReason']=finish
                response=io.BytesIO(json.dumps({'candidates':[candidate]}).encode())
                with patch.dict(os.environ,env),patch.object(renderer,'urlopen',return_value=response):
                    speech=renderer.Speech('gemini')
                    try:
                        with self.assertRaisesRegex(RuntimeError,'did not finish successfully'):
                            speech.speak('This complete script must not accompany partial audio.',0,'/unused')
                    finally:speech.close()
        for failure in (TimeoutError('test-private-key'),URLError(TimeoutError('test-private-key'))):
            with patch.dict(os.environ,env),patch.object(renderer,'urlopen',side_effect=failure):
                speech=renderer.Speech('gemini')
                try:
                    with self.assertRaisesRegex(RuntimeError,'timed out') as raised:
                        speech.speak('A timeout test.',0,'/unused')
                    self.assertNotIn('test-private-key',str(raised.exception))
                finally:speech.close()


@unittest.skipUnless(os.environ.get('STUDIO_RUN_MEDIA_TESTS') == '1', 'Set STUDIO_RUN_MEDIA_TESTS=1 for optional thermally gated media tests')
class MediaTests(unittest.TestCase):
    def run_cli(self,data,directory,filename='output'):
        source=Path(directory)/f'{filename}-request.json';target=Path(directory)/f'{filename}.mp4'
        source.write_text(json.dumps(data))
        run=subprocess.run([sys.executable,str(Path(renderer.__file__)), '--input',str(source),'--output',str(target)],
                           capture_output=True,text=True,timeout=60)
        self.assertEqual(run.returncode,0,run.stderr)
        events=[json.loads(line) for line in run.stdout.splitlines()]
        self.assertTrue(all(type(event['progress']) is int for event in events), 'Go progress contract requires integer percentages')
        self.assertEqual(events[-1]['stage'],'completed')
        self.assertEqual(events[-1]['progress'],100)
        return target

    def test_all_formats_produce_audio_video_and_captions(self):
        with tempfile.TemporaryDirectory() as tmp:
            for format,dimensions in [('portrait',(360,640)),('landscape',(640,360)),('square',(360,360))]:
                with self.subTest(format=format):
                    target=self.run_cli(request(format),tmp,format)
                    metadata=json.loads(target.with_suffix('.json').read_text())
                    self.assertEqual((metadata['width'],metadata['height']),dimensions)
                    self.assertGreater(metadata['duration_seconds'],1)
                    self.assertTrue(target.with_suffix('.jpg').stat().st_size>1000)
                    self.assertIn('Make one clear point.',target.with_suffix('.srt').read_text())
                    decode=subprocess.run(['ffmpeg','-v','error','-threads','2','-i',str(target),'-f','null','-'],capture_output=True,timeout=30)
                    self.assertEqual(decode.returncode,0,decode.stderr)
                    pcm=subprocess.run(['ffmpeg','-v','error','-threads','2','-i',str(target),'-vn','-ac','1','-f','s16le','pipe:1'],capture_output=True,timeout=30,check=True).stdout
                    samples=array.array('h',pcm)
                    self.assertGreater(max(abs(v) for v in samples),100)
                    # Head motion, mouth states, and captions must produce changing frames.
                    hashes=subprocess.run(['ffmpeg','-v','error','-threads','2','-i',str(target),'-an','-f','framemd5','pipe:1'],capture_output=True,text=True,timeout=30,check=True)
                    frames=[line.rsplit(',',1)[-1] for line in hashes.stdout.splitlines() if line and not line.startswith('#')]
                    self.assertGreater(len(set(frames)),10)

    def test_cancel_removes_partial_files_and_child_encoder(self):
        with tempfile.TemporaryDirectory() as tmp:
            data=request(quality='preview');data['draft']['scenes']*=12
            source=Path(tmp)/'request.json';source.write_text(json.dumps(data))
            target=Path(tmp)/'canceled.mp4'
            process=subprocess.Popen([sys.executable,renderer.__file__,'--input',str(source),'--output',str(target)],
                                     stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            children=[]
            try:
                while True:
                    readable,_,_=select.select([process.stdout],[],[],30)
                    self.assertTrue(readable,'Renderer did not start within 30 seconds')
                    line=process.stdout.readline()
                    self.assertTrue(line,'Renderer exited before animation')
                    if json.loads(line)['stage']=='rendering':break
                child_file=Path(f'/proc/{process.pid}/task/{process.pid}/children')
                if child_file.exists():children=child_file.read_text().split()
                process.send_signal(signal.SIGTERM)
                stdout,stderr=process.communicate(timeout=8)
                self.assertNotEqual(process.returncode,0)
                self.assertIn('canceled',stderr.lower())
                self.assertFalse(target.exists())
                self.assertFalse(list(Path(tmp).glob('.studio-render-*')))
                for child in children:self.assertFalse(Path(f'/proc/{child}').exists(),f'Encoder child {child} survived cancellation')
            finally:
                if process.poll() is None:process.kill();process.communicate(timeout=5)

    def test_selected_piper_service_failure_is_reported(self):
        with patch.dict(os.environ,{'STUDIO_TTS_PROVIDER':'piper','ALLOW_LOCAL_MODELS':'true','TTS_BASE_URL':'http://127.0.0.1:1'}):
            cap=renderer.capabilities()
            self.assertFalse(cap['ready'])
            self.assertEqual(cap['voice_engine'],'piper-http')
            self.assertIn('Piper is selected but unavailable',cap['reason'])

    def test_piper_http_contract_and_estimated_caption_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            speech=renderer.Speech('espeak')
            try:pcm,rate,_=speech.speak('This is a spoken service test.',0,tmp)
            finally:speech.close()
            audio=io.BytesIO()
            with wave.open(audio,'wb') as wav:
                wav.setnchannels(1);wav.setsampwidth(2);wav.setframerate(rate);wav.writeframes(pcm)
            requests=[]
            def fake_open(url, timeout):
                if isinstance(url,Request):
                    self.assertEqual(url.full_url,'http://piper.test/api/tts')
                    self.assertEqual(url.method,'POST')
                    requests.append(json.loads(url.data))
                    response=io.BytesIO(audio.getvalue())
                else:
                    body={'status':'ok'} if url.endswith('/healthz') else {'voices':[{'key':'test-a'},{'key':'test-b'}]}
                    response=io.BytesIO(json.dumps(body).encode())
                response.status=200
                return response
            env={'STUDIO_TTS_PROVIDER':'piper','ALLOW_LOCAL_MODELS':'true','TTS_BASE_URL':'http://piper.test',
                 'STUDIO_VOICE_A':'test-a','STUDIO_VOICE_B':'test-b'}
            with patch.dict(os.environ,env),patch.object(renderer,'urlopen',side_effect=fake_open):
                target=Path(tmp)/'piper.mp4'
                with redirect_stdout(io.StringIO()):
                    data=request();data['voice_provider']='piper'
                    renderer.render(data,target)
                    metadata=json.loads(target.with_suffix('.json').read_text())
                    self.assertEqual(metadata['voice_engine'],'piper-http')
                    self.assertEqual(metadata['caption_timing'],'estimated-word-timing')
                    self.assertEqual([r['voice_key'] for r in requests],['test-a','test-b'])
                    with patch.dict(os.environ,{'STUDIO_VOICE_A':'missing-voice'}):
                        with self.assertRaisesRegex(RuntimeError,'voice keys are unavailable'):renderer.Speech()


if __name__=='__main__':unittest.main()
