import io
import unittest
import wave
from types import SimpleNamespace
from unittest.mock import Mock, patch

import numpy as np
import torch

from acoustic_classifier import (
    LAUGHTER,
    SPEECH,
    UNKNOWN,
    TransformersAudioClassifier,
    _inputs_to_device,
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

    def test_each_classification_logs_the_scores_it_decided_on(self):
        classifier = TransformersAudioClassifier("fixture-model")
        classifier._processor = Mock(return_value={})
        classifier._model = Mock(
            return_value=SimpleNamespace(logits=torch.tensor([[0.0, 2.0, -2.0]]))
        )
        classifier._model.config = SimpleNamespace(
            id2label={0: "Speech", 1: "Laughter", 2: "Music"}
        )
        classifier._model.eval = Mock()

        with self.assertLogs("dokja_voice.acoustic", level="INFO") as logs:
            classifier.classify(wav_bytes())

        line = logs.records[-1].getMessage()
        self.assertIn("kind=laughter", line)
        self.assertIn("laughter=0.88", line)
        self.assertIn("speech=0.50", line)
        self.assertIn("top=laughter:0.88", line)

    def test_disabled_or_silent_classification_is_unknown(self):
        classifier = TransformersAudioClassifier("")
        self.assertEqual(classifier.classify(wav_bytes()), UNKNOWN)
        self.assertEqual(classifier.classify(b""), UNKNOWN)

    def test_rocm_requires_an_available_accelerator(self):
        with patch("acoustic_classifier.torch.cuda.is_available", return_value=False):
            with self.assertRaisesRegex(RuntimeError, "accelerator.*unavailable"):
                TransformersAudioClassifier("fixture-model", device="rocm")
        with patch("acoustic_classifier.torch.cuda.is_available", return_value=True):
            classifier = TransformersAudioClassifier("fixture-model", device="rocm")
            self.assertEqual(str(classifier.device), "cuda")

    def test_rocm_loads_the_model_and_inputs_on_the_selected_device(self):
        model = Mock()
        with patch(
            "acoustic_classifier.torch.cuda.is_available", return_value=True
        ), patch("acoustic_classifier.AutoFeatureExtractor.from_pretrained"), patch(
            "acoustic_classifier.AutoModelForAudioClassification.from_pretrained",
            return_value=model,
        ):
            classifier = TransformersAudioClassifier("fixture-model", device="rocm")
            classifier._load()
        model.to.assert_called_once_with(torch.device("cuda"))
        tensor = Mock(spec=torch.Tensor)
        tensor.to.return_value = tensor
        inputs = _inputs_to_device({"input_values": tensor}, classifier.device)
        self.assertEqual(inputs["input_values"], tensor)
        tensor.to.assert_called_once_with(torch.device("cuda"))


if __name__ == "__main__":
    unittest.main()
