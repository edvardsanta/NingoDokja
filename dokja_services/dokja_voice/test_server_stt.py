import base64
import os
import unittest
from unittest.mock import patch

from fastapi.testclient import TestClient

from server import create_app


class STTResponseTests(unittest.TestCase):
    def test_stt_returns_the_acoustic_kind_without_rewriting_the_provider_text(self):
        environment = {
            "DOKJA_VOICE_PROVIDER": "stub",
            "DOKJA_VOICE_STT_PROVIDER": "whisper_cpp",
            "DOKJA_VOICE_CPP_ENDPOINT": "http://localhost:8085",
            "DOKJA_VOICE_ACOUSTIC_CLASSIFIER": "transformers",
        }
        with patch.dict(os.environ, environment), patch(
            "server.WhisperCppSTTService"
        ) as stt_type, patch("server.TransformersAudioClassifier") as classifier_type:
            stt_type.return_value.transcribe.return_value = "E aí"
            classifier_type.return_value.classify.return_value = "laughter"
            response = TestClient(create_app()).post(
                "/stt",
                json={
                    "audio_bytes_b64": base64.b64encode(b"audio").decode(),
                    "audio_format": "wav",
                    "language": "pt-BR",
                },
            )

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["text"], "E aí")
        self.assertEqual(response.json()["audioKind"], "laughter")
        classifier_type.return_value.classify.assert_called_once()

    def test_classifier_failure_preserves_a_valid_transcription_as_unknown(self):
        environment = {
            "DOKJA_VOICE_PROVIDER": "stub",
            "DOKJA_VOICE_STT_PROVIDER": "whisper_cpp",
            "DOKJA_VOICE_CPP_ENDPOINT": "http://localhost:8085",
            "DOKJA_VOICE_ACOUSTIC_CLASSIFIER": "transformers",
        }
        with patch.dict(os.environ, environment), patch(
            "server.WhisperCppSTTService"
        ) as stt_type, patch("server.TransformersAudioClassifier") as classifier_type:
            stt_type.return_value.transcribe.return_value = "transcrição válida"
            classifier_type.return_value.classify.side_effect = RuntimeError(
                "unavailable"
            )
            response = TestClient(create_app()).post(
                "/stt",
                json={
                    "audio_bytes_b64": base64.b64encode(b"audio").decode(),
                    "audio_format": "wav",
                    "language": "pt-BR",
                },
            )

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["text"], "transcrição válida")
        self.assertEqual(response.json()["audioKind"], "unknown")

    def test_empty_compose_value_preserves_provider_specific_defaults(self):
        for provider, expected in [
            ("whisper_cpp", "transformers"),
            ("stub", "disabled"),
        ]:
            with self.subTest(provider=provider), patch.dict(
                os.environ,
                {
                    "DOKJA_VOICE_PROVIDER": "stub",
                    "DOKJA_VOICE_STT_PROVIDER": provider,
                    "DOKJA_VOICE_CPP_ENDPOINT": "http://localhost:8085",
                    "DOKJA_VOICE_ACOUSTIC_CLASSIFIER": "",
                },
            ), patch("server.WhisperCppSTTService"), patch(
                "server.TransformersAudioClassifier"
            ):
                response = TestClient(create_app()).get("/health")
                self.assertEqual(response.json()["acoustic_classifier"], expected)


if __name__ == "__main__":
    unittest.main()
