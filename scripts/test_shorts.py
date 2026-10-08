import random
import os
from contextlib import chdir
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

from shorts import plan_clips, write_captions, cool_down, require_space, render, probe, run


class SegmentationTests(unittest.TestCase):
    def test_thermal_pause_uses_hysteresis(self):
        with patch("shorts.temperature", side_effect=[85, 78, 69]), patch("shorts.time.sleep") as sleep, patch("shorts.emit"), patch.dict("os.environ", {"SHORTS_PAUSE_TEMP_C": "80"}):
            cool_down()
            self.assertEqual(sleep.call_count, 2)

    def test_missing_sensor_does_not_block(self):
        with patch("shorts.temperature", return_value=None), patch("shorts.time.sleep") as sleep:
            cool_down()
            sleep.assert_not_called()

    def test_disk_reserve_stops_processing(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict("os.environ", {"SHORTS_MIN_FREE_GB": "999999999"}):
            with self.assertRaisesRegex(RuntimeError, "free disk space"):
                require_space(directory)

    def test_entire_timeline_retained_with_no_clip_count_limit(self):
        for duration in [0.05, 5, 44.99, 45, 46, 90, 3600, 18000]:
            clips = plan_clips(duration, [])
            self.assertEqual(clips[0][0], 0)
            self.assertAlmostEqual(clips[-1][1], duration)
            for i, (start, end) in enumerate(clips):
                self.assertGreater(end, start)
                self.assertLessEqual(end - start, 45)
                if i:
                    self.assertEqual(start, clips[i - 1][1])
        self.assertGreater(len(plan_clips(18000, [])), 100)

    def test_sentence_boundary_preferred(self):
        words = [{"start": 29, "end": 30, "word": "done."},
                 {"start": 30.1, "end": 31, "word": "Next"}]
        clips = plan_clips(80, words)
        self.assertAlmostEqual(clips[0][1], 30.05)
        self.assertEqual(clips[-1][1], 80)

    def test_fixed_presets_cover_two_hours_including_tail(self):
        words = [{"start": 20, "end": 21, "word": "stop."}]
        for maximum in [5, 15, 30, 40, 45]:
            clips = plan_clips(7200, words, maximum, "fixed")
            self.assertAlmostEqual(clips[0][1], maximum - 0.08)
            self.assertEqual(clips[0][0], 0)
            self.assertEqual(clips[-1][1], 7200)
            self.assertAlmostEqual(sum(b-a for a, b in clips), 7200)
            self.assertTrue(all(a == previous[1] for previous, (a, _) in zip(clips, clips[1:])))
            self.assertTrue(all(0 < b-a <= maximum for a, b in clips))

    def test_single_edit_uses_source_time_and_keeps_short_tail(self):
        self.assertEqual(plan_clips(7200, [], 40, "sentence", 60), [(60, 99.92)])
        self.assertEqual(plan_clips(7200, [], 30, "fixed", 7198), [(7198, 7200)])
        for start in [-1, 7200, float("nan")]:
            with self.assertRaises(ValueError):
                plan_clips(7200, [], 30, "fixed", start)

    def test_rounding_does_not_create_an_unrenderable_extra_clip(self):
        clips = plan_clips(787.2, [], 5, "fixed")
        self.assertEqual(len(clips), 160)
        self.assertEqual(clips[-1][1], 787.2)

    def test_invalid_duration_is_rejected_before_planning(self):
        for maximum in [0, -1, 46, float("nan")]:
            with self.assertRaises(ValueError):
                plan_clips(100, [], maximum)

    def test_reel_captions_animate_and_escape_text(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "captions.ass"
            write_captions(path, [{"start": 61, "end": 62, "word": r"{\pos(0,0)}"}], 60, 65, "reel")
            text = path.read_text()
            self.assertIn("0:00:01.00,0:00:02.00", text)
            self.assertIn(r"\t(", text)
            self.assertNotIn(r"{\pos(0,0)}", text)

    def test_random_timestamps_never_drop_content_or_exceed_limit(self):
        rng = random.Random(10)
        for _ in range(40):
            duration = rng.uniform(50, 1000)
            words = [{"start": i, "end": i + 0.3, "word": "word." if i % 7 == 0 else "word"}
                     for i in range(int(duration))]
            clips = plan_clips(duration, words, 15)
            self.assertAlmostEqual(sum(b-a for a, b in clips), duration)
            self.assertTrue(all(0 < b-a <= 15 for a, b in clips))

    def test_caption_times_are_clip_relative_and_text_is_escaped(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "captions.ass"
            write_captions(path, [{"start": 44, "end": 46, "word": r"{\pos(0,0)}"}], 45, 46)
            text = path.read_text()
            self.assertIn("0:00:00.00,0:00:01.00", text)
            self.assertNotIn(r"{\pos(0,0)}", text)


@unittest.skipUnless(os.getenv("STUDIO_RUN_MEDIA_TESTS") == "1", "Opt-in FFmpeg media checks")
class RenderTests(unittest.TestCase):
    def test_reel_layouts_have_playable_video_audio_and_burned_captions(self):
        cool_down()
        with tempfile.TemporaryDirectory() as directory, chdir(directory):
            source = Path(directory) / "source.mp4"
            run(["ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30",
                 "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "3",
                 "-c:v", "libx264", "-threads", "1", "-preset", "ultrafast", "-c:a", "aac", str(source)])
            write_captions(Path("captions.ass"), [{"start": 0.5, "end": 2, "word": "Caption test"}], 0.5, 2, "reel")
            for layout in ["fit", "crop", "split"]:
                cool_down()
                output = Path(directory) / f"{layout}.mp4"
                render(source, output, "captions.ass", layout, 1.5, 0.5, False, "reel")
                info = probe(output)
                video = next(s for s in info["streams"] if s["codec_type"] == "video")
                audio = next(s for s in info["streams"] if s["codec_type"] == "audio")
                self.assertEqual((video["width"], video["height"], video["codec_name"]), (1080, 1920, "h264"))
                self.assertEqual(audio["codec_name"], "aac")
                self.assertGreater(float(info["format"]["duration"]), 1.4)
                self.assertLess(float(info["format"]["duration"]), 1.58)
            cool_down()
            render(source, Path("uncaptioned.mp4"), None, "fit", 1.5, 0.5, False, "reel")
            def frame_hash(name):
                return run(["ffmpeg", "-v", "error", "-ss", "0.5", "-i", name, "-map", "0:v:0", "-frames:v", "1", "-f", "hash", "-"])
            self.assertNotEqual(frame_hash("fit.mp4"), frame_hash("uncaptioned.mp4"))


if __name__ == "__main__":
    unittest.main()
