import unittest
from unittest.mock import patch

import numpy as np

from speech_activity import SpeechDetector


class SpeechActivityTests(unittest.TestCase):
    def test_digital_silence_and_isolated_keyboard_impulses_are_not_speech(self):
        detector = SpeechDetector()
        silence = np.zeros(16000, dtype=np.int16)
        self.assertFalse(detector.detect(silence)["speech"])
        clicks = silence.copy()
        clicks[::1600] = 12000
        self.assertFalse(detector.detect(clicks)["speech"])

    def test_stationary_room_noise_is_rejected(self):
        noise = np.random.default_rng(7).normal(0, 350, 16000).astype(np.int16)
        self.assertFalse(SpeechDetector().detect(noise)["speech"])

    def test_sustained_speech_frames_are_required_for_a_probe(self):
        audio = np.zeros(5120, dtype=np.int16)
        with patch("speech_activity.webrtcvad.Vad") as vad:
            vad.return_value.is_speech.return_value = True
            result = SpeechDetector().detect(audio, strict=True)
            self.assertTrue(result["speech"])
            self.assertEqual(result["speech_ms"], 320)
            self.assertEqual(vad.call_args.args, (3,))
        with patch("speech_activity.webrtcvad.Vad") as vad:
            vad.return_value.is_speech.side_effect = [True] * 3 + [False] * 13
            self.assertFalse(SpeechDetector().detect(audio, strict=True)["speech"])


if __name__ == "__main__":
    unittest.main()
