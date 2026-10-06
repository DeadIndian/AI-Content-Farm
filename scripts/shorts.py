#!/usr/bin/env python3
"""Local podcast transcription, sentence-aware segmentation and vertical rendering.

One JSON progress event per stdout line; diagnostics go to stderr.
"""
import argparse
import bisect
import gc
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import time


def threads():
    return max(1, min(4, int(os.getenv("SHORTS_THREADS", "2"))))


def temperature():
    readings = []
    for label in Path("/sys/class/thermal").glob("thermal_zone*/type"):
        try:
            if label.read_text().strip() in ("x86_pkg_temp", "cpu-thermal"):
                readings.append(float(label.with_name("temp").read_text()) / 1000)
        except (OSError, ValueError):
            pass
    return max(readings) if readings else None


def cool_down():
    threshold = float(os.getenv("SHORTS_PAUSE_TEMP_C", "80"))
    current = temperature()
    if threshold <= 0 or current is None or current < threshold:
        return
    emit(warning=f"CPU temperature {current:.0f}°C; waiting to cool below {threshold-10:.0f}°C")
    while current is not None and current > threshold - 10:
        time.sleep(5)
        current = temperature()


def require_space(path):
    import shutil
    minimum = float(os.getenv("SHORTS_MIN_FREE_GB", "5")) * 1024**3
    if shutil.disk_usage(path).free < minimum:
        raise RuntimeError("Insufficient free disk space; free space before retrying this job")


def atomic_json(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")
    temporary.replace(path)


def emit(**event):
    print(json.dumps(event, ensure_ascii=False), flush=True)


def run(args):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed: {result.stderr[-6000:]}")
    return result.stdout


def probe(path):
    return json.loads(run([os.getenv("FFPROBE_BIN", "ffprobe"), "-v", "error", "-show_format",
                           "-show_streams", "-of", "json", str(path)]))


def plan_clips(duration, words, maximum=45):
    """Cover the entire timeline, keeping the tail and preferring sentence/pause cuts."""
    # Reserve one frame plus an AAC packet so muxed duration never exceeds the cap.
    limit = maximum - 0.08
    boundaries = []
    for i, word in enumerate(words):
        end = float(word["end"])
        next_start = float(words[i + 1]["start"]) if i + 1 < len(words) else end
        if re.search(r"[.!?。！？।][\"'’”]*$", word["word"].strip()) or next_start - end >= 0.45:
            boundaries.append(end + min(0.12, max(0, next_start - end) / 2))
    clips, start = [], 0.0
    word_ends = sorted(float(w["end"]) for w in words)
    boundaries.sort()
    while start < duration - 0.001:
        end = min(duration, start + limit)
        if end < duration:
            candidates = boundaries[bisect.bisect_left(boundaries, start + limit * 0.55):bisect.bisect_right(boundaries, end)]
            if candidates:
                end = max(candidates)
            else:
                # Prefer a word boundary over cutting a word in half.
                candidates = word_ends[bisect.bisect_left(word_ends, start + limit * 0.8):bisect.bisect_right(word_ends, end)]
                if candidates:
                    end = max(candidates)
        clips.append((start, end))
        start = end
    return clips


def ass_time(seconds):
    value = max(0, round(seconds * 100))
    return f"{value // 360000}:{value // 6000 % 60:02}:{value // 100 % 60:02}.{value % 100:02}"


def safe_text(text):
    return text.replace("\\", "＼").replace("{", "(").replace("}", ")").replace("\n", " ").strip()


def write_captions(path, words, start, end):
    header = """[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 0

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,DejaVu Sans,68,&H00FFFFFF,&H00FFFFFF,&H00101010,&H80000000,-1,0,0,0,100,100,0,0,1,5,2,2,85,85,380,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
"""
    selected = [w for w in words if w["end"] > start and w["start"] < end]
    groups, group = [], []
    for word in selected:
        if group and (len(group) >= 4 or len(" ".join(w["word"] for w in group)) + len(word["word"]) > 30
                      or word["start"] - group[-1]["end"] > 0.6):
            groups.append(group)
            group = []
        group.append(word)
    if group:
        groups.append(group)
    lines = [header]
    for group in groups:
        for i, word in enumerate(group):
            a = max(start, word["start"])
            b = min(end, group[i + 1]["start"] if i + 1 < len(group) else word["end"])
            if b <= a:
                continue
            text = " ".join(("{\\c&H00D7FF&}" + safe_text(w["word"]) + "{\\c&HFFFFFF&}")
                            if j == i else safe_text(w["word"]) for j, w in enumerate(group))
            lines.append(f"Dialogue: 0,{ass_time(a-start)},{ass_time(b-start)},Default,,0,0,0,,{text}\n")
    path.write_text("".join(lines), encoding="utf-8")


def transcribe(source, work, language, model_name):
    cache = work / "transcript.json"
    backend = os.getenv("SHORTS_TRANSCRIBER", "cpu")
    if backend not in ("cpu", "auto", "npu"):
        raise RuntimeError("SHORTS_TRANSCRIBER must be cpu, auto or npu")
    signature = {"size": source.stat().st_size, "mtime": source.stat().st_mtime_ns,
                 "model": model_name, "language": language, "backend": backend, "version": 3}
    if cache.exists():
        saved = json.loads(cache.read_text())
        if saved.get("signature") == signature:
            return saved
    emit(stage="transcribing", progress=8, message="Loading local speech model (first run downloads model weights)")
    cool_down()
    from faster_whisper import WhisperModel
    audio = work / "audio.wav"
    duration = float(probe(source)["format"]["duration"])
    model = None
    actual_backend = "cpu"
    if backend in ("auto", "npu"):
        try:
            from npu_transcribe import NPUTranscriber
            model = NPUTranscriber(model_name)
            actual_backend = "npu"
            emit(stage="transcribing", progress=8, message="Transcribing on Intel NPU (OpenVINO, word timestamps)")
        except Exception as exc:
            if backend == "npu":
                raise RuntimeError(f"NPU transcription unavailable: {exc}") from exc
            emit(warning=f"NPU unavailable; using CPU int8 transcription: {exc}")
    if model is None:
        model = WhisperModel(model_name, device="cpu", compute_type="int8",
                             cpu_threads=threads(), num_workers=1)
    words = []
    detected = language or None
    # Decode only a minute at a time. Overlap gives words at chunk edges context;
    # midpoint ownership prevents duplication across the overlapping windows.
    for start in range(0, math.ceil(duration), 60):
        cool_down()
        require_space(work)
        chunk_cache = work / f"transcript-chunk-{start:08d}.json"
        chunk = json.loads(chunk_cache.read_text()) if chunk_cache.exists() else {}
        if chunk.get("signature") != signature:
            offset, end = max(0, start - 2), min(duration, start + 62)
            run([os.getenv("FFMPEG_BIN", "ffmpeg"), "-v", "error", "-nostdin", "-y",
                 "-threads", str(threads()), "-ss", str(offset), "-i", str(source),
                 "-t", str(end-offset), "-vn", "-ac", "1", "-ar", "16000", str(audio)])
            try:
                segments, info = model.transcribe(str(audio), language=detected,
                                                  word_timestamps=True, vad_filter=True, beam_size=3,
                                                  condition_on_previous_text=False)
            except Exception as exc:
                if backend != "auto" or isinstance(model, WhisperModel):
                    raise
                emit(warning=f"NPU inference failed; retrying on CPU: {exc}")
                del model
                gc.collect()
                model = WhisperModel(model_name, device="cpu", compute_type="int8", cpu_threads=threads(), num_workers=1)
                actual_backend = "cpu"
                segments, info = model.transcribe(str(audio), language=detected, word_timestamps=True,
                                                  vad_filter=True, beam_size=3, condition_on_previous_text=False)
            chunk_words = []
            for segment in segments:
                for w in segment.words or []:
                    midpoint = offset + (w.start + w.end) / 2
                    if w.end > w.start and w.word.strip() and start <= midpoint < min(duration, start+60):
                        chunk_words.append({"start": offset+w.start, "end": min(duration, offset+w.end), "word": w.word.strip()})
            chunk = {"signature": signature, "language": info.language, "words": chunk_words, "backend": actual_backend}
            atomic_json(chunk_cache, chunk)
            audio.unlink(missing_ok=True)
        words.extend(chunk["words"])
        if chunk["words"]:
            detected = chunk["language"]
        emit(stage="transcribing", progress=8 + int(32 * min((start+60) / duration, 1)),
             message=f"Transcribed {min(start+60, duration):.0f} / {duration:.0f} seconds ({chunk.get('backend', actual_backend)}, low-memory mode)")
    result = {"signature": signature, "language": detected or language, "words": words, "backend": actual_backend}
    atomic_json(cache, result)
    audio.unlink(missing_ok=True)
    del model
    gc.collect()
    return result


def encoder_available(ffmpeg):
    if not Path("/dev/dri/renderD128").exists():
        return False
    try:
        run([ffmpeg, "-v", "error", "-nostdin", "-init_hw_device", "vaapi=va:/dev/dri/renderD128",
             "-f", "lavfi", "-i", "color=s=64x64:d=0.1", "-vf", "format=nv12,hwupload",
             "-c:v", "h264_vaapi", "-f", "null", "-"])
        return True
    except RuntimeError:
        return False


def video_filter(layout, captions):
    if layout == "crop":
        graph = "[0:v]scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,setsar=1[base]"
    elif layout == "split":
        graph = ("[0:v]split=2[left][right];"
                 "[left]crop=iw/2:ih:0:0,scale=1080:960:force_original_aspect_ratio=increase,crop=1080:960,setsar=1[top];"
                 "[right]crop=iw/2:ih:iw/2:0,scale=1080:960:force_original_aspect_ratio=increase,crop=1080:960,setsar=1[bottom];"
                 "[top][bottom]vstack[base]")
    else:
        graph = ("[0:v]split=2[bg][fg];"
                 "[bg]scale=270:480:force_original_aspect_ratio=increase,crop=270:480,boxblur=12:2,scale=1080:1920,setsar=1[blur];"
                 "[fg]scale=1080:1920:force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1[front];"
                 "[blur][front]overlay=(W-w)/2:(H-h)/2[base]")
    # Caption filename is generated by us and relative to the subprocess cwd.
    graph += ";[base]fps=30"
    if captions:
        graph += f",ass={captions}"
    return graph


def render(source, output, caption, layout, duration, start, hardware):
    ffmpeg = os.getenv("FFMPEG_BIN", "ffmpeg")
    args = [ffmpeg, "-v", "error", "-nostdin", "-y", "-filter_complex_threads", "1", "-threads", str(threads())]
    if hardware:
        args += ["-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-filter_hw_device", "va"]
    args += ["-ss", f"{start:.6f}", "-i", str(source)]
    graph = video_filter(layout, caption)
    graph += ",format=nv12,hwupload[v]" if hardware else ",format=yuv420p[v]"
    args += ["-filter_complex", graph, "-map", "[v]", "-map", "0:a:0", "-t", f"{duration:.6f}"]
    if hardware:
        args += ["-c:v", "h264_vaapi", "-qp", "23"]
    else:
        args += ["-c:v", "libx264", "-preset", "veryfast", "-crf", "22", "-threads", str(threads())]
    args += ["-af", "aresample=async=1:first_pts=0", "-c:a", "aac", "-b:a", "160k",
             "-movflags", "+faststart", "-map_metadata", "-1", str(output)]
    run(args)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--work", required=True)
    parser.add_argument("--prefix", required=True)
    parser.add_argument("--layout", choices=["fit", "crop", "split"], default="fit")
    parser.add_argument("--language", default="")
    parser.add_argument("--model", default=os.getenv("WHISPER_MODEL", "base"))
    parser.add_argument("--max-duration", type=float, default=45)
    parser.add_argument("--no-captions", action="store_true")
    parser.add_argument("--encoder", choices=["auto", "cpu", "vaapi"], default=os.getenv("SHORTS_ENCODER", "auto"))
    args = parser.parse_args()
    if not 5 <= args.max_duration <= 45:
        parser.error("max duration must be between 5 and 45 seconds")
    source, output, work = Path(args.source).resolve(), Path(args.output).resolve(), Path(args.work).resolve()
    output.mkdir(parents=True, exist_ok=True)
    work.mkdir(parents=True, exist_ok=True)
    require_space(work)
    require_space(output)
    try:
        os.nice(10)
    except OSError:
        pass
    if temperature() is None:
        emit(warning="CPU temperature sensor unavailable; Docker CPU/memory limits still apply.")
    os.chdir(work)
    metadata = probe(source)
    if not any(s["codec_type"] == "video" for s in metadata["streams"]) or not any(s["codec_type"] == "audio" for s in metadata["streams"]):
        raise RuntimeError("Source must contain both video and audio")
    duration = float(metadata["format"]["duration"])
    if not math.isfinite(duration) or duration <= 0:
        raise RuntimeError("Source has no finite duration")
    transcript = {"words": [], "language": args.language}
    if not args.no_captions:
        transcript = transcribe(source, work, args.language, args.model)
        if not transcript["words"]:
            emit(warning="No speech detected; rendering without captions.")
    words = transcript["words"]
    clips = plan_clips(duration, words, args.max_duration)
    hardware = args.encoder != "cpu" and encoder_available(os.getenv("FFMPEG_BIN", "ffmpeg"))
    if args.encoder == "vaapi" and not hardware:
        raise RuntimeError("Intel VAAPI encoder unavailable; use auto or cpu")
    emit(stage="rendering", progress=40, total=len(clips), source_duration=duration,
         message=f"Rendering {len(clips)} shorts using {'Intel GPU' if hardware else 'CPU'}")
    results = []
    for index, (start, end) in enumerate(clips, 1):
        cool_down()
        require_space(output)
        name = f"{args.prefix}-{index:04d}.mp4"
        destination = output / name
        caption = f"captions-{index:04d}.ass"
        write_captions(work / caption, words, start, end)
        temporary = work / "rendering.mp4"
        if not destination.exists():
            try:
                render(source, temporary, caption if words else None, args.layout, end-start, start, hardware)
            except RuntimeError:
                if not hardware or args.encoder == "vaapi":
                    raise
                hardware = False
                emit(warning="GPU render failed; retrying this clip using CPU.")
                render(source, temporary, caption if words else None, args.layout, end-start, start, False)
            actual = float(probe(temporary)["format"]["duration"])
            if actual > args.max_duration:
                raise RuntimeError(f"Clip exceeded duration limit: {actual}")
            # Work and output can be different filesystems.
            import shutil
            shutil.move(str(temporary), str(destination))
        actual = float(probe(destination)["format"]["duration"])
        if actual > args.max_duration:
            raise RuntimeError(f"Existing clip exceeded duration limit: {actual}")
        text = " ".join(w["word"] for w in words if start <= w["start"] < end)
        clip = {"filename": name, "url": "/outputs/" + name, "start": start, "end": end,
                "duration": actual, "title": " ".join(text.split()[:12]) or f"Part {index}", "text": text}
        results.append(clip)
        emit(stage="rendering", progress=40 + int(59 * index / len(clips)), total=len(clips),
             completed=index, clip=clip, message=f"Rendered {index} / {len(clips)} shorts")
        if index < len(clips):
            time.sleep(max(0, float(os.getenv("SHORTS_CLIP_REST_SECONDS", "2"))))
    manifest = {"source_duration": duration, "language": transcript["language"], "clips": results}
    (output / f"{args.prefix}.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
    emit(stage="completed", progress=100, message=f"Created {len(results)} shorts", total=len(results), completed=len(results))


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
