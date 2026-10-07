"""Planner tests cover explicit content modes, grounding and durable graph runs."""
import importlib.util
import io
import json
import os
import sqlite3
from contextlib import closing
import tempfile
import unittest
from unittest.mock import patch

import studio_plan as planner


class PlannerTests(unittest.TestCase):
    def request(self, **overrides):
        return {"topic": planner.DEMO_TOPICS[0], "provider": "demo", "pair": "nova-atlas",
                "format": "portrait", "target_seconds": 60, "source_notes": "", **overrides}

    def test_no_silent_demo_for_unknown_topic(self):
        with self.assertRaisesRegex(ValueError, "Demo mode supports only"):
            planner.validate_request(self.request(topic="Last night's election results"))

    def test_manual_preserves_sentence_content(self):
        notes = "This first sentence makes a complete point. The second sentence develops another idea."
        scenes = planner.validate_scenes(planner.manual_scenes(notes))
        self.assertEqual([scene["speaker"] for scene in scenes], [0, 1])
        self.assertEqual(" ".join(scene["text"] for scene in scenes), notes)
        self.assertEqual(scenes[0]["text"], "This first sentence makes a complete point.")

    def test_empty_manual_rejected(self):
        with self.assertRaisesRegex(ValueError, "requires source_notes"):
            planner.validate_request(self.request(provider="manual"))

    def test_strict_scene_shape(self):
        for invalid in [[], [{"speaker": True}], [{"speaker": 0.0, "text": "a", "visual": "b"}]*2,
                        [{"speaker": 0, "text": "hello", "visual": "card"}]*2]:
            with self.subTest(scenes=invalid), self.assertRaises(ValueError):
                planner.validate_scenes(invalid)

    def test_duration_and_options_are_bounded(self):
        for update in [{"target_seconds": 600}, {"target_seconds": True}, {"format": "giant"},
                       {"pair": "../../etc"}, {"source_notes": "x"*18001}]:
            with self.subTest(update=update), self.assertRaises(ValueError):
                planner.validate_request(self.request(**update))

    def test_ai_requires_configuration(self):
        with patch.dict(os.environ, {}, clear=True), self.assertRaisesRegex(ValueError, "AI mode requires"):
            planner.ai_script(self.request(provider="ai"))

    def test_ai_request_contains_grounding_and_structured_contract(self):
        response = {"choices": [{"message": {"content": json.dumps({"title": "A title", "scenes": []})}}]}
        captured = []
        def fake_open(request, timeout):
            captured.append((request, timeout))
            return io.BytesIO(json.dumps(response).encode())
        with patch.dict(os.environ, {"LLM_API_KEY": "test-secret", "LLM_MODEL": "test-model"}, clear=True), \
             patch("urllib.request.urlopen", side_effect=fake_open):
            result = planner.ai_script(self.request(provider="ai", source_notes="The sample has exactly three layers."))
        self.assertEqual(result["title"], "A title")
        request, timeout = captured[0]
        payload = json.loads(request.data)
        self.assertEqual(timeout, 45)
        self.assertEqual(payload["response_format"]["type"], "json_object")
        self.assertIn("use only their factual claims", payload["messages"][0]["content"])
        self.assertIn("exactly three layers", payload["messages"][1]["content"])

    def test_provider_malformed_response_is_explicit_error(self):
        with patch.dict(os.environ, {"LLM_API_KEY": "test-secret", "LLM_MODEL": "test-model"}, clear=True), \
             patch("urllib.request.urlopen", return_value=io.BytesIO(b'{"error":"nope"}')), \
             self.assertRaisesRegex(ValueError, "invalid structured script"):
            planner.ai_script(self.request(provider="ai"))

    def test_gemini_uses_configured_cast_and_explicit_structured_contract(self):
        envelope = {"candidates": [{"finishReason": "STOP", "content": {"parts": [
            {"text": json.dumps({"title": "Cloud script", "scenes": []})}]}}]}
        with patch.dict(os.environ, {"GEMINI_API_KEY": "test-secret"}, clear=True), \
             patch("urllib.request.urlopen", return_value=io.BytesIO(json.dumps(envelope).encode())) as send:
            result = planner.ai_script(self.request(provider="ai", pair="cog-axiom"))
        request = send.call_args.args[0]
        payload = json.loads(request.data)
        self.assertEqual(result["title"], "Cloud script")
        self.assertNotIn("test-secret", request.full_url)
        self.assertEqual(request.get_header("X-goog-api-key"), "test-secret")
        self.assertEqual(payload["generationConfig"]["responseMimeType"], "application/json")
        cast = json.loads(payload["contents"][0]["parts"][0]["text"])["cast"]
        self.assertEqual([speaker["name"] for speaker in cast], ["Cog", "Axiom"])

    def test_unknown_cast_and_incomplete_gemini_output_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "Unknown presenter pair|unavailable"):
            planner.validate_request(self.request(pair="not-installed"))
        envelope = {"candidates": [{"finishReason": "MAX_TOKENS"}]}
        with patch.dict(os.environ, {"GEMINI_API_KEY": "test-secret"}, clear=True), \
             patch("urllib.request.urlopen", return_value=io.BytesIO(json.dumps(envelope).encode())), \
             self.assertRaisesRegex(ValueError, "did not complete"):
            planner.ai_script(self.request(provider="ai"))

    def test_references_are_only_user_supplied(self):
        state = {"request": self.request(provider="ai", source_notes="Reference https://example.org/paper. Three layers."), "warnings": []}
        result = planner.gather_context(state)
        self.assertEqual(result["sources"], [{"title": "User-supplied reference", "url": "https://example.org/paper"}])
        self.assertIn("No web research", result["warnings"][0])

    @unittest.skipUnless(importlib.util.find_spec("langgraph"), "LangGraph not installed")
    def test_actual_graph_runs_all_nodes_and_saves_checkpoint(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "planning.sqlite3")
            result = planner.plan(self.request(), path)
            self.assertEqual([event["node"] for event in result["trace"]], ["brief", "source_context", "script", "validate", "review"])
            self.assertEqual(result["provider"], "demo")
            self.assertIn("Awaiting human review", result["trace"][-1]["detail"])
            with closing(sqlite3.connect(path)) as db:
                self.assertGreater(db.execute("SELECT count(*) FROM checkpoints").fetchone()[0], 0)

    @unittest.skipUnless(importlib.util.find_spec("langgraph"), "LangGraph not installed")
    def test_graph_rejects_invalid_provider_output_without_fallback(self):
        with patch.object(planner, "ai_script", return_value={"title": "Bad", "scenes": []}), \
             self.assertRaisesRegex(ValueError, "2 to 24 scenes"):
            planner.plan(self.request(provider="ai"))


if __name__ == "__main__":
    unittest.main()
