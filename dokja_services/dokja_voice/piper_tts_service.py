import io
import os
import wave

from piper import PiperVoice


class PiperTTSService:
    def __init__(
        self,
        model_path: str,
        default_voice: str = "default",
        default_language: str = "pt-BR",
    ) -> None:
        if not model_path:
            raise ValueError("DOKJA_VOICE_PIPER_MODEL is required when provider=piper")
        if not os.path.exists(model_path):
            raise ValueError(f"piper model not found: {model_path}")

        self.model_path = model_path
        self.default_voice = default_voice
        self.default_language = default_language
        self.voice = PiperVoice.load(model_path)

    def synthesize(self, text: str, voice: str, language: str, output_format: str) -> bytes:
        if output_format != "wav":
            raise ValueError("piper backend currently supports wav output only")

        buffer = io.BytesIO()
        with wave.open(buffer, "wb") as wav_file:
            self.voice.synthesize_wav(text.strip(), wav_file)
        return buffer.getvalue()
