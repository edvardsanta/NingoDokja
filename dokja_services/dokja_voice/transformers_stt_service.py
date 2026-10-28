import base64
import logging
import math
import threading
import time

import numpy as np
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor, pipeline

from audio_utils import wav_bytes_to_mono_16k


class TransformersSTTService:
    def __init__(
        self,
        model_name: str,
        default_language: str = "pt-BR",
        model_cache_dir: str | None = None,
    ) -> None:
        if not model_name.strip():
            raise ValueError("DOKJA_VOICE_STT_MODEL is required when stt provider=transformers")

        cache_dir = model_cache_dir.strip() if model_cache_dir else None
        self._inference_lock = threading.Lock()
        self.default_language = default_language
        self.model_name = model_name
        self.processor = AutoProcessor.from_pretrained(model_name, cache_dir=cache_dir)
        self.model = AutoModelForSpeechSeq2Seq.from_pretrained(model_name, cache_dir=cache_dir)
        self.pipeline = pipeline(
            "automatic-speech-recognition",
            model=self.model,
            tokenizer=self.processor.tokenizer,
            feature_extractor=self.processor.feature_extractor,
            device=-1,
        )

    def transcribe(self, audio_bytes: bytes, audio_format: str, language: str) -> str:
        if audio_format != "wav":
            raise ValueError("transformers stt service currently supports wav input only")
        if not audio_bytes:
            return ""

        # Discord supplies stereo 48 kHz PCM. Normalize in memory so silent
        # packets can be rejected before Whisper tries to generate text for them.
        pcm = wav_bytes_to_mono_16k(audio_bytes)
        if pcm.size < 4000 or np.max(np.abs(pcm.astype(np.int32))) <= 32:
            return ""
        samples = pcm.astype(np.float32) / 32768.0
        duration = samples.size / 16000
        # Short/noisy audio can otherwise consume Whisper's full context while
        # hallucinating repeated words. Twelve tokens/second leaves headroom for
        # ordinary speech while bounding the work for each Discord segment.
        token_limit = max(16, min(128, math.ceil(duration * 12)))
        normalized_language = language.strip() or self.default_language
        started = time.monotonic()
        with self._inference_lock:
            result = self.pipeline(
                {"raw": samples, "sampling_rate": 16000},
                generate_kwargs={
                    "language": _normalize_whisper_language(normalized_language),
                    "task": "transcribe",
                    "max_new_tokens": token_limit,
                    "num_beams": 1,
                    "do_sample": False,
                    "condition_on_prev_tokens": False,
                },
            )
        text = str(result.get("text", "")) if isinstance(result, dict) else str(result)
        logging.getLogger("dokja_voice.stt").info(
            "stt completed audio_seconds=%.3f elapsed_ms=%d text_chars=%d token_limit=%d",
            duration,
            round((time.monotonic() - started) * 1000),
            len(text),
            token_limit,
        )
        return text.strip()


def decode_audio_b64(payload: str) -> bytes:
    try:
        return base64.b64decode(payload.encode("utf-8"), validate=True)
    except Exception as exc:
        raise ValueError("audio_bytes_b64 is not valid base64") from exc


def _normalize_whisper_language(language: str) -> str:
    normalized = language.strip().lower()
    if normalized in {"pt-br", "pt_br", "pt"}:
        return "portuguese"
    if normalized in {"en-us", "en_us", "en"}:
        return "english"
    return normalized
