import io
import threading
import unittest
import wave
from unittest.mock import Mock

import numpy as np

from transformers_stt_service import TransformersSTTService


def wav_bytes(seconds, channels=2, rate=48000, amplitude=2000):
    samples = np.arange(round(seconds * rate), dtype=np.float32)
    pcm = (amplitude * np.sin(samples * (2 * np.pi * 220 / rate))).astype(np.int16)
    if channels > 1:
        pcm = np.repeat(pcm[:, None], channels, axis=1)
    output = io.BytesIO()
    with wave.open(output, "wb") as wav:
        wav.setnchannels(channels)
        wav.setsampwidth(2)
        wav.setframerate(rate)
        wav.writeframes(pcm.tobytes())
    return output.getvalue()


class TranscriptionTests(unittest.TestCase):
    def setUp(self):
        self.service = TransformersSTTService.__new__(TransformersSTTService)
        self.service.default_language = "pt-BR"
        self.service._inference_lock = threading.Lock()
        self.service.pipeline = Mock(return_value={"text": "  recognized speech  "})

    def test_short_audio_and_digital_silence_never_reach_decoder(self):
        for audio in [b"", wav_bytes(0.14), wav_bytes(2, amplitude=0)]:
            with self.subTest(size=len(audio)):
                self.assertEqual(self.service.transcribe(audio, "wav", "pt-BR"), "")
        self.service.pipeline.assert_not_called()

    def test_stereo_discord_audio_is_normalized_and_decode_is_bounded(self):
        result = self.service.transcribe(wav_bytes(2), "wav", "pt-BR")
        self.assertEqual(result, "recognized speech")
        args, kwargs = self.service.pipeline.call_args
        self.assertEqual(args[0]["sampling_rate"], 16000)
        self.assertEqual(args[0]["raw"].shape, (32000,))
        self.assertEqual(args[0]["raw"].dtype, np.float32)
        self.assertLessEqual(np.abs(args[0]["raw"]).max(), 1)
        generation = kwargs["generate_kwargs"]
        self.assertEqual(generation["language"], "portuguese")
        self.assertEqual(generation["max_new_tokens"], 24)
        self.assertFalse(generation["condition_on_prev_tokens"])
        self.assertFalse(generation["do_sample"])

    def test_decode_budget_has_an_absolute_cap(self):
        self.service.transcribe(wav_bytes(20, channels=1, rate=16000), "wav", "en")
        generation = self.service.pipeline.call_args.kwargs["generate_kwargs"]
        self.assertEqual(generation["max_new_tokens"], 128)
        self.assertEqual(generation["language"], "english")

    def test_invalid_format_does_not_reach_model(self):
        with self.assertRaises(ValueError):
            self.service.transcribe(b"audio", "mp3", "pt-BR")
        self.service.pipeline.assert_not_called()


if __name__ == "__main__":
    unittest.main()
