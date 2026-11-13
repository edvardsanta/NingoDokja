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


def kind_from_scores(scores: dict[str, float]) -> str:
    normalized = {label.strip().lower(): score for label, score in scores.items()}
    laughter = sum(
        score for label, score in normalized.items() if label in _LAUGHTER_LABELS
    )
    speech = sum(
        score for label, score in normalized.items() if label in _SPEECH_LABELS
    )
    if laughter >= 0.65 and laughter >= speech * 1.25:
        return LAUGHTER
    if speech >= 0.35 and speech >= laughter * 1.25:
        return SPEECH
    return UNKNOWN


class TransformersAudioClassifier:
    def __init__(self, model_name: str, model_cache_dir: str | None = None) -> None:
        self.model_name = model_name.strip()
        self.model_cache_dir = model_cache_dir.strip() if model_cache_dir else None
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
            with torch.no_grad():
                logits = self._model(**inputs).logits
            probabilities = torch.sigmoid(logits)[0].tolist()
        labels = self._model.config.id2label
        scores = {
            str(labels[index]).strip().lower(): float(score)
            for index, score in enumerate(probabilities)
            if index in labels
        }
        return kind_from_scores(scores)

    def _load(self) -> None:
        if self._model is not None:
            return
        self._processor = AutoFeatureExtractor.from_pretrained(
            self.model_name, cache_dir=self.model_cache_dir
        )
        self._model = AutoModelForAudioClassification.from_pretrained(
            self.model_name, cache_dir=self.model_cache_dir
        )
        self._model.eval()
        logging.getLogger("dokja_voice.acoustic").info(
            "acoustic classifier loaded model=%s", self.model_name
        )
