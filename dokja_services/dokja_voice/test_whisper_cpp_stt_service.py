import io
import json
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
        for body in [b'not json', b'{"text":42}', b'{"error":"provider failure"}', b'{"segments":["bad"]}', b'x' * 262145]:
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

    def test_text_whisper_invents_from_background_noise_is_dropped(self):
        def segment(text, logprob, *probabilities):
            words = [{"word": f" w{i}", "probability": p} for i, p in enumerate(probabilities)]
            return {"text": text, "avg_logprob": logprob, "words": words}

        # Scores measured from this provider: clear speech, and steady noise.
        body = json.dumps({"text": "ignored", "segments": [
            segment(" Achei isso. Vou na frente.", -0.06, 0.97, 0.99, 0.82),
            segment(" Fahona.", -1.3, 0.31),
            segment(" A Lana e Ningo.", -0.5, 0.35, 0.4, 0.3),
            segment(" .", -0.7, 0.12),
        ]}).encode()
        service = self.service(body)
        with self.assertLogs("dokja_voice.stt") as logs:
            text = service.transcribe(wav_bytes(1), "wav", "pt")
        self.assertEqual(text, "Achei isso. Vou na frente.")
        self.assertIn(b'\r\n\r\nverbose_json\r\n', service._http.open.call_args.args[0].data)
        self.assertIn("segments_kept=1 segments_dropped=3", logs.output[0])
        self.assertNotIn("Fahona", logs.output[0], "the log carries scores, never transcript text")

    def test_only_noise_yields_no_transcript(self):
        body = b'{"segments":[{"text":" Obrigado.","avg_logprob":-1.1,"words":[{"word":" Obrigado","probability":0.2}]}]}'
        self.assertEqual(self.service(body).transcribe(wav_bytes(1), "wav", "pt"), "")

    def test_a_plain_text_result_is_still_accepted(self):
        self.assertEqual(self.service(b'{"text":"  recognized  "}').transcribe(wav_bytes(1), "wav", "pt"), "recognized")


if __name__ == "__main__":
    unittest.main()
