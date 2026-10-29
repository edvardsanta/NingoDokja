import json
import unittest

from voice_latency_report import summarize


def event(stage, ms, sentence=None):
    item = {"traceId": "synthetic", "stage": stage, "elapsedMs": ms}
    if sentence:
        item["sentenceId"] = sentence
    return "container | [dokja-discord] voice timing " + json.dumps(item)


class ReportTests(unittest.TestCase):
    def test_prefetched_sentences_remain_independent_and_duplicates_are_ignored(self):
        lines = [event("capture_started", 0),
                 event("tts_requested", 10, "one"), event("tts_ready", 40, "one"),
                 event("tts_requested", 15, "two"), event("tts_ready", 65, "two"),
                 event("playback_started", 50, "one"), event("playback_started", 100, "two")]
        result = summarize(lines + lines)
        self.assertEqual(result["metrics"]["tts_ms"],
                         {"samples": 2, "p50": 30, "p95": 50, "p99": 50})
        self.assertEqual(result["metrics"]["capture_to_first_player_start_ms"]["p50"], 50)

    def test_compose_ansi_color_wrappers_are_accepted(self):
        line = "\x1b[37m" + event("capture_started", 0) + "\x1b[0m\n"
        result = summarize([line])
        self.assertEqual(result["traces"], 1)
        self.assertEqual(result["malformed_events"], 0)

    def test_incomplete_and_failed_events_are_reported_without_fake_latency(self):
        result = summarize([event("tts_requested", 10, "one"),
                            event("tts_failed", 20, "one"),
                            '[dokja-discord] voice timing {bad json',
                            event("capture_started", float("nan"))])
        self.assertEqual(result["traces_without_playback"], 1)
        self.assertEqual(result["metrics"]["tts_ms"]["samples"], 0)
        self.assertEqual(result["failures"], {"tts_failed": 1})
        self.assertEqual(result["malformed_events"], 2)


if __name__ == "__main__":
    unittest.main()
