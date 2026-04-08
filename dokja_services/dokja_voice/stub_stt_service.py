class StubSTTService:
    def __init__(self, default_language: str = "pt-BR") -> None:
        self.default_language = default_language

    def transcribe(self, audio_bytes: bytes, audio_format: str, language: str) -> str:
        normalized_language = language.strip() or self.default_language
        if audio_format != "wav":
            raise ValueError("stub stt service only supports wav input")
        if not audio_bytes:
            return ""
        size_hint = len(audio_bytes)
        return f"[stub-{normalized_language}] audio_turn_{size_hint}_bytes"
