import os

import numpy as np

from audio_utils import wav_bytes_to_mono_16k


class OpenWakeWordService:
    def __init__(
        self,
        model_path: str,
        hotword: str = "Ningo",
        threshold: float = 0.5,
    ) -> None:
        if not model_path.strip():
            raise ValueError("DOKJA_VOICE_WAKEWORD_MODEL is required when wakeword provider=openwakeword")
        if not os.path.exists(model_path):
            raise ValueError(f"wakeword model not found: {model_path}")

        try:
            from openwakeword.model import Model
        except Exception as exc:
            raise ValueError("openwakeword is not installed") from exc

        self.hotword = hotword
        self.threshold = threshold
        self.model_path = model_path
        self.model = Model(wakeword_models=[model_path], inference_framework="onnx")
        self.model_key = self._infer_model_key(model_path)

    def detect(self, audio_bytes: bytes, audio_format: str, hotword: str | None = None) -> dict:
        if audio_format != "wav":
            raise ValueError("openwakeword service currently supports wav input only")

        pcm = wav_bytes_to_mono_16k(audio_bytes)
        if pcm.size == 0:
            return {
                "detected": False,
                "score": 0.0,
                "hotword": hotword or self.hotword,
                "provider": "openwakeword",
            }

        self._reset_model()
        best_score = 0.0
        frame_size = 1280

        for idx in range(0, pcm.shape[0], frame_size):
            frame = pcm[idx : idx + frame_size]
            if frame.shape[0] < frame_size:
                padded = np.zeros(frame_size, dtype=np.int16)
                padded[: frame.shape[0]] = frame
                frame = padded
            scores = self.model.predict(frame)
            score = self._extract_score(scores)
            if score > best_score:
                best_score = score

        return {
            "detected": best_score >= self.threshold,
            "score": float(best_score),
            "hotword": hotword or self.hotword,
            "provider": "openwakeword",
        }

    def _infer_model_key(self, model_path: str) -> str:
        filename = os.path.splitext(os.path.basename(model_path))[0]
        normalized = filename.lower()
        if normalized.endswith(".onnx"):
            normalized = normalized[:-5]
        return normalized

    def _extract_score(self, scores: dict) -> float:
        if not isinstance(scores, dict):
            return 0.0
        if self.model_key in scores:
            return float(scores[self.model_key])
        if scores:
            return float(max(scores.values()))
        return 0.0

    def _reset_model(self) -> None:
        if hasattr(self.model, "reset"):
            self.model.reset()
