class StubWakewordService:
    def __init__(self, hotword: str = "Ningo") -> None:
        self.hotword = hotword

    def detect(self, audio_bytes: bytes, audio_format: str, hotword: str | None = None) -> dict:
        if audio_format != "wav":
            raise ValueError("stub wakeword service only supports wav input")
        normalized_hotword = (hotword or self.hotword).strip() or self.hotword
        return {
            "detected": True,
            "score": 1.0,
            "hotword": normalized_hotword,
            "provider": "stub",
        }
