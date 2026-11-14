import logging
import threading

import numpy as np
import torch
from transformers import AutoFeatureExtractor, AutoModelForAudioClassification

from audio_utils import wav_bytes_to_mono_16k

SPEECH = "speech"
LAUGHTER = "laughter"
UNKNOWN = "unknown"

_LAUGHTER_LABELS = {
    "laughter",
    "giggle",
    "chuckle, chortle",
    "snicker",
    "belly laugh",
}
_SPEECH_LABELS = {
    "speech",
    "male speech, man speaking",
    "female speech, woman speaking",
    "conversation",
    "narration, monologue",
}


def laughter_and_speech(scores: dict[str, float]) -> tuple[float, float]:
    """The summed laughter and speech scores of one clip."""
    normalized = {label.strip().lower(): score for label, score in scores.items()}
    laughter = sum(
        score for label, score in normalized.items() if label in _LAUGHTER_LABELS
    )
    speech = sum(
        score for label, score in normalized.items() if label in _SPEECH_LABELS
    )
    return laughter, speech


def kind_from_scores(scores: dict[str, float]) -> str:
    laughter, speech = laughter_and_speech(scores)
    if laughter >= 0.65 and laughter >= speech * 1.25:
        return LAUGHTER
    if speech >= 0.35 and speech >= laughter * 1.25:
        return SPEECH
    return UNKNOWN


def _resolve_device(requested: str) -> torch.device:
    normalized = requested.strip().lower() or "cpu"
    if normalized == "auto":
        return torch.device("cuda" if torch.cuda.is_available() else "cpu")
    if normalized == "cpu":
        return torch.device("cpu")
    if normalized in {"cuda", "rocm", "gpu"}:
        if not torch.cuda.is_available():
            raise RuntimeError(
                "acoustic classifier accelerator was requested but is unavailable"
            )
        return torch.device("cuda")
    raise ValueError("DOKJA_VOICE_ACOUSTIC_DEVICE supports cpu, auto or rocm")


def _inputs_to_device(inputs: dict, device: torch.device) -> dict:
    return {
        key: value.to(device) if isinstance(value, torch.Tensor) else value
        for key, value in inputs.items()
    }


class TransformersAudioClassifier:
    def __init__(
        self,
        model_name: str,
        model_cache_dir: str | None = None,
        device: str = "cpu",
    ) -> None:
        self.model_name = model_name.strip()
        self.model_cache_dir = model_cache_dir.strip() if model_cache_dir else None
        self.device = (
            _resolve_device(device) if self.model_name else torch.device("cpu")
        )
        self._lock = threading.Lock()
        self._processor = None
        self._model = None

    def classify(self, audio_bytes: bytes) -> str:
        if not self.model_name or not audio_bytes:
            return UNKNOWN
        pcm = wav_bytes_to_mono_16k(audio_bytes)
        if pcm.size < 4000 or np.max(np.abs(pcm.astype(np.int32))) <= 32:
            return UNKNOWN
        with self._lock:
            self._load()
            samples = pcm.astype(np.float32) / 32768.0
            inputs = self._processor(samples, sampling_rate=16000, return_tensors="pt")
            inputs = _inputs_to_device(inputs, self.device)
            with torch.no_grad():
                logits = self._model(**inputs).logits
            probabilities = torch.sigmoid(logits)[0].tolist()
        labels = self._model.config.id2label
        scores = {
            str(labels[index]).strip().lower(): float(score)
            for index, score in enumerate(probabilities)
            if index in labels
        }
        kind = kind_from_scores(scores)
        laughter, speech = laughter_and_speech(scores)
        top_label, top_score = max(scores.items(), key=lambda item: item[1])
        # Only numbers and a label, never audio or text: this is what tuning the thresholds needs.
        logging.getLogger("dokja_voice.acoustic").info(
            "acoustic classified kind=%s laughter=%.2f speech=%.2f top=%s:%.2f seconds=%.2f",
            kind,
            laughter,
            speech,
            top_label,
            top_score,
            pcm.size / 16000,
        )
        return kind

    def _load(self) -> None:
        if self._model is not None:
            return
        self._processor = AutoFeatureExtractor.from_pretrained(
            self.model_name, cache_dir=self.model_cache_dir
        )
        self._model = AutoModelForAudioClassification.from_pretrained(
            self.model_name, cache_dir=self.model_cache_dir
        )
        self._model.to(self.device)
        self._model.eval()
        logging.getLogger("dokja_voice.acoustic").info(
            "acoustic classifier loaded model=%s device=%s",
            self.model_name,
            self.device,
        )
