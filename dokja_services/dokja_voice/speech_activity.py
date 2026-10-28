import numpy as np
import webrtcvad


class SpeechDetector:
    """Stateless WebRTC decisions on PCM16 mono 16 kHz audio."""

    def detect(self, pcm: np.ndarray, strict: bool = False) -> dict:
        detector = webrtcvad.Vad(3)
        frame_samples = 320  # 20 ms at 16 kHz
        total = len(pcm) // frame_samples
        voiced = 0
        run = 0
        longest = 0
        for index in range(total):
            frame = pcm[index * frame_samples : (index + 1) * frame_samples]
            if detector.is_speech(frame.astype("<i2", copy=False).tobytes(), 16000):
                voiced += 1
                run += 1
                longest = max(longest, run)
            else:
                run = 0
        ratio = voiced / total if total else 0.0
        speech = voiced >= 8 and longest >= 6 and ratio >= (0.55 if strict else 0.25)
        return {
            "speech": speech,
            "speech_ms": voiced * 20,
            "audio_ms": total * 20,
            "speech_ratio": round(ratio, 3),
        }
