import io
import math
import wave


class StubTTSService:
    def __init__(self, default_voice: str = "default", default_language: str = "pt-BR") -> None:
        self.default_voice = default_voice
        self.default_language = default_language

    def synthesize(self, text: str, voice: str, language: str, output_format: str) -> bytes:
        if output_format != "wav":
            raise ValueError("stub service only supports wav output")

        normalized_voice = voice.strip() or self.default_voice
        normalized_language = language.strip() or self.default_language
        duration_seconds = min(max(len(text) / 40.0, 1.0), 6.0)
        frequency = 440 if normalized_language.startswith("pt") else 523
        if normalized_voice != self.default_voice:
            frequency += 40
        return self._generate_wav(duration_seconds=duration_seconds, frequency=frequency)

    def _generate_wav(self, duration_seconds: float, frequency: int) -> bytes:
        sample_rate = 24000
        amplitude = 16000
        total_frames = int(sample_rate * duration_seconds)
        buffer = io.BytesIO()

        with wave.open(buffer, "wb") as wav_file:
            wav_file.setnchannels(1)
            wav_file.setsampwidth(2)
            wav_file.setframerate(sample_rate)

            frames = bytearray()
            for i in range(total_frames):
                envelope = 0.4 if i % 4800 < 3200 else 0.15
                sample = int(amplitude * envelope * math.sin(2 * math.pi * frequency * i / sample_rate))
                frames.extend(sample.to_bytes(2, byteorder="little", signed=True))
            wav_file.writeframes(frames)

        return buffer.getvalue()
