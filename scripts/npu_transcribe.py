"""OpenVINO NPU adapter with real word timestamps and CPU voice detection."""
from pathlib import Path
from types import SimpleNamespace
import os
import wave

import numpy as np


class NPUTranscriber:
    def __init__(self, model_name):
        import openvino as ov
        import openvino_genai as genai
        from huggingface_hub import snapshot_download

        if "NPU" not in ov.Core().available_devices:
            raise RuntimeError("Intel NPU is unavailable to OpenVINO")
        if model_name != "base":
            raise RuntimeError("The verified NPU model is whisper-base; select CPU for other models")
        root = Path(os.getenv("HF_HOME", "data/models"))
        model = snapshot_download(
            "OpenVINO/whisper-base-fp16-ov",
            revision="84fbe975a79a8c996fd32c036558f29e2db6670f",
            local_dir=root / "openvino-whisper-base",
            allow_patterns=["*.xml", "*.bin", "*.json", "*.txt"],
            max_workers=1,
        )
        self.pipeline = genai.WhisperPipeline(
            model, "NPU", word_timestamps=True, CACHE_DIR=str(root / "npu-cache"))

    def transcribe(self, path, language=None, **_):
        from faster_whisper.vad import get_speech_timestamps, VadOptions

        with wave.open(path) as audio:
            samples = np.frombuffer(audio.readframes(audio.getnframes()), dtype="<i2").astype(np.float32) / 32768
        # Whisper can hallucinate speech on silence; retain lightweight VAD.
        speech = get_speech_timestamps(samples, VadOptions())
        if not speech:
            return [], SimpleNamespace(language=language or "")
        options = {"word_timestamps": True, "return_timestamps": True,
                   "max_new_tokens": 448, "task": "transcribe"}
        if language:
            options["language"] = f"<|{language}|>"
        result = self.pipeline.generate(samples, **options)
        words = [SimpleNamespace(start=w.start_ts, end=w.end_ts, word=w.word)
                 for w in result.words
                 if any(s["start"]/16000 - .2 <= (w.start_ts+w.end_ts)/2 <= s["end"]/16000 + .2 for s in speech)]
        return [SimpleNamespace(words=words)], SimpleNamespace(language=result.language)
