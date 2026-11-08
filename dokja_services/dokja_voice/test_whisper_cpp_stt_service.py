import io
import unittest
import wave
from unittest.mock import Mock

from whisper_cpp_stt_service import WhisperCppSTTService
from test_transformers_stt_service import wav_bytes


class LocalGPUTranscriptionTests(unittest.TestCase):
    def service(self, body=b'{"text":"  recognized  "}'):
        service = WhisperCppSTTService("http://localhost:8085/inference")
        service.speech_detector = Mock()
        service.speech_detector.detect.return_value = {"speech": True}
        response = Mock()
        response.read.return_value = body
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        service._http = Mock()
        service._http.open.return_value = response
        return service

    def test_only_normalized_wav_reaches_the_configured_provider(self):
        service = self.service()
        self.assertEqual(service.transcribe(wav_bytes(1), "wav", "pt-BR"), "recognized")
        request = service._http.open.call_args.args[0]
        self.assertEqual(request.full_url, "http://localhost:8085/inference")
        self.assertEqual(service._http.open.call_args.kwargs["timeout"], 10)
        data = request.data
        self.assertIn(b'\r\n\r\npt\r\n', data)
        wav_start = data.index(b"RIFF")
        with wave.open(io.BytesIO(data[wav_start:]), "rb") as audio:
            self.assertEqual((audio.getnchannels(), audio.getframerate(), audio.getsampwidth()), (1, 16000, 2))
            self.assertEqual(audio.getnframes(), 16000)

    def test_silence_short_audio_and_non_speech_make_no_network_request(self):
        service = self.service()
        self.assertEqual(service.transcribe(b"", "wav", "pt"), "")
        self.assertEqual(service.transcribe(wav_bytes(1, amplitude=0), "wav", "pt"), "")
        self.assertEqual(service.transcribe(wav_bytes(.1), "wav", "pt"), "")
        service.speech_detector.detect.return_value = {"speech": False}
        self.assertEqual(service.transcribe(wav_bytes(1), "wav", "pt"), "")
        service._http.open.assert_not_called()

    def test_malformed_or_oversized_provider_output_fails_without_speech_fallback(self):
        for body in [b'not json', b'{"text":42}', b'{"error":"provider failure"}', b'x' * 65537]:
            with self.subTest(body_len=len(body)):
                with self.assertRaisesRegex(RuntimeError, "local GPU transcription failed"):
                    self.service(body).transcribe(wav_bytes(1), "wav", "pt")

    def test_endpoint_is_explicit_and_segment_work_is_bounded(self):
        for endpoint in ["", "file:///model", "http://user:password@localhost/inference", "http://localhost/path?secret=value"]:
            with self.assertRaises(ValueError):
                WhisperCppSTTService(endpoint)
        service = self.service()
        for args in [(b"audio", "mp3", "pt"), (wav_bytes(31), "wav", "pt"), (wav_bytes(1), "wav", "unsupported")]:
            with self.assertRaises(ValueError):
                service.transcribe(*args)
        service._http.open.assert_not_called()


if __name__ == "__main__":
    unittest.main()
