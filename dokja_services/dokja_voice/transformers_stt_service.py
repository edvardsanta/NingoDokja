import base64
import os
from tempfile import NamedTemporaryFile

from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor, pipeline


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

        normalized_language = language.strip() or self.default_language
        suffix = f".{audio_format}"
        with NamedTemporaryFile(suffix=suffix, delete=False) as temp_file:
            temp_file.write(audio_bytes)
            temp_path = temp_file.name

        try:
            result = self.pipeline(
                temp_path,
                generate_kwargs={"language": _normalize_whisper_language(normalized_language)},
            )
            if isinstance(result, dict):
                return str(result.get("text", "")).strip()
            return str(result).strip()
        finally:
            try:
                os.unlink(temp_path)
            except FileNotFoundError:
                pass


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
