import io
import unittest
import wave
from types import SimpleNamespace
from unittest.mock import Mock

import numpy as np
import torch

from acoustic_classifier import (
    LAUGHTER,
    SPEECH,
    UNKNOWN,
    TransformersAudioClassifier,
    kind_from_scores,
)


def wav_bytes(seconds=1):
    samples = np.arange(round(seconds * 16000), dtype=np.float32)
    pcm = (2000 * np.sin(samples * (2 * np.pi * 220 / 16000))).astype(np.int16)
    output = io.BytesIO()
    with wave.open(output, "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(16000)
        wav.writeframes(pcm.tobytes())
    return output.getvalue()


class AcousticClassifierTests(unittest.TestCase):
    def test_laughter_requires_a_clear_margin_over_speech(self):
        self.assertEqual(kind_from_scores({"Laughter": 0.8, "Speech": 0.2}), LAUGHTER)
        self.assertEqual(kind_from_scores({"Laughter": 0.8, "Speech": 0.75}), UNKNOWN)
        self.assertEqual(kind_from_scores({"Laughter": 0.1, "Speech": 0.7}), SPEECH)
        self.assertEqual(kind_from_scores({"Music": 0.9}), UNKNOWN)

    def test_audio_labels_not_transcribed_words_select_laughter(self):
        classifier = TransformersAudioClassifier("fixture-model")
        classifier._processor = Mock(return_value={})
        classifier._model = Mock(
            return_value=SimpleNamespace(logits=torch.tensor([[0.0, 2.0, -2.0]]))
        )
        classifier._model.config = SimpleNamespace(
            id2label={0: "Speech", 1: "Laughter", 2: "Music"}
        )
        classifier._model.eval = Mock()

        self.assertEqual(classifier.classify(wav_bytes()), LAUGHTER)
        samples = classifier._processor.call_args.args[0]
        self.assertEqual(samples.shape, (16000,))
        self.assertEqual(classifier._processor.call_args.kwargs["sampling_rate"], 16000)

    def test_disabled_or_silent_classification_is_unknown(self):
        classifier = TransformersAudioClassifier("")
        self.assertEqual(classifier.classify(wav_bytes()), UNKNOWN)
        self.assertEqual(classifier.classify(b""), UNKNOWN)


if __name__ == "__main__":
    unittest.main()
