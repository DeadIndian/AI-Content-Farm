import random
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

from shorts import plan_clips, write_captions, cool_down, require_space


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


if __name__ == "__main__":
    unittest.main()
