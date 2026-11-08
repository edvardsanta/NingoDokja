"""Bounded stateless transport to an explicitly configured local Whisper server."""
import io
import json
import logging
import math
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import wave

import numpy as np

from audio_utils import wav_bytes_to_mono_16k
from speech_activity import SpeechDetector


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


class WhisperCppSTTService:
    def __init__(self, endpoint: str, default_language: str = "pt-BR") -> None:
        target = urllib.parse.urlsplit(endpoint)
        if (target.scheme not in {"http", "https"} or not target.hostname
                or target.username or target.password or target.fragment or target.query):
            raise ValueError("DOKJA_VOICE_CPP_ENDPOINT requires an explicit HTTP inference endpoint")
        self.endpoint = endpoint
        self.default_language = default_language
        self.speech_detector = SpeechDetector()
        self._http = urllib.request.build_opener(_NoRedirect())

    def transcribe(self, audio_bytes: bytes, audio_format: str, language: str) -> str:
        if audio_format != "wav":
            raise ValueError("local GPU transcription requires WAV input")
        if not audio_bytes:
            return ""
        pcm = wav_bytes_to_mono_16k(audio_bytes)
        if pcm.size > 30 * 16000:
            raise ValueError("transcription segment exceeds 30 seconds")
        if pcm.size < 4000 or np.max(np.abs(pcm.astype(np.int32))) <= 32:
            return ""
        if not self.speech_detector.detect(pcm)["speech"]:
            return ""
        selected_language = (language.strip() or self.default_language).lower().replace("_", "-").split("-")[0]
        if selected_language not in {"pt", "en", "auto"}:
            raise ValueError("local GPU transcription supports pt, en or auto")
        audio = io.BytesIO()
        with wave.open(audio, "wb") as wav:
            wav.setnchannels(1)
            wav.setsampwidth(2)
            wav.setframerate(16000)
            wav.writeframes(pcm.astype("<i2", copy=False).tobytes())
        boundary = uuid.uuid4().hex
        fields = {"language": selected_language, "response_format": "json",
                  "temperature": "0", "temperature_inc": "0", "max_context": "0"}
        chunks = []
        for key, value in fields.items():
            chunks.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
        chunks.extend([
            f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="speech.wav"\r\nContent-Type: audio/wav\r\n\r\n'.encode(),
            audio.getvalue(), f'\r\n--{boundary}--\r\n'.encode(),
        ])
        request = urllib.request.Request(self.endpoint, data=b"".join(chunks), headers={
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        })
        started = time.monotonic()
        try:
            with self._http.open(request, timeout=10) as response:
                raw = response.read(65537)
            if len(raw) > 65536:
                raise ValueError("transcription response exceeds limit")
            result = json.loads(raw)
            text = result.get("text") if isinstance(result, dict) else None
            if not isinstance(text, str) or len(text) > 16000:
                raise ValueError("invalid transcription response")
        except (OSError, urllib.error.URLError, ValueError) as exc:
            # No provider envelopes, endpoints, audio or transcript content in errors.
            raise RuntimeError("local GPU transcription failed") from exc
        samples = pcm.astype(np.float32) / 32768
        rms = float(np.sqrt(np.mean(samples * samples)))
        logging.getLogger("dokja_voice.stt").info(
            "stt completed provider=whisper_cpp audio_seconds=%.3f elapsed_ms=%d text_chars=%d rms_dbfs=%.1f peak_dbfs=%.1f clipped_pct=%.2f",
            pcm.size / 16000, round((time.monotonic() - started) * 1000), len(text),
            20 * math.log10(max(rms, 1e-6)),
            20 * math.log10(max(float(np.max(np.abs(samples))), 1e-6)),
            float(np.mean(np.abs(pcm.astype(np.int32)) >= 32767) * 100),
        )
        return text.strip()
