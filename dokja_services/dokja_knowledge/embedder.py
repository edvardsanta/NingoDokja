"""Turns text into vectors with a local Ollama server (bge-m3 by default)."""

from __future__ import annotations

import json
import logging
import urllib.error
import urllib.request

import numpy as np

logger = logging.getLogger("dokja_knowledge.embedder")

DEFAULT_ENDPOINT = "http://127.0.0.1:11434"
DEFAULT_MODEL = "bge-m3"


class EmbedUnavailable(RuntimeError):
    """The embedding server could not produce vectors (down, model missing, bad reply)."""


def normalize(matrix: np.ndarray) -> np.ndarray:
    """L2-normalize rows, so a dot product is the cosine similarity."""
    matrix = np.asarray(matrix, dtype=np.float32)
    norms = np.linalg.norm(matrix, axis=1, keepdims=True)
    norms[norms == 0] = 1.0
    return matrix / norms


class OllamaEmbedder:
    def __init__(self, endpoint: str = DEFAULT_ENDPOINT, model: str = DEFAULT_MODEL,
                 timeout: float = 60.0, batch_size: int = 16):
        self.endpoint = endpoint.rstrip("/")
        self.model = model
        self.timeout = timeout
        self.batch_size = batch_size

    def embed(self, texts: list[str]) -> np.ndarray:
        """Return one normalized vector per text, in order. Raises EmbedUnavailable."""
        if not texts:
            return np.zeros((0, 0), dtype=np.float32)
        vectors: list[list[float]] = []
        for start in range(0, len(texts), self.batch_size):
            vectors.extend(self._embed_batch(texts[start : start + self.batch_size]))
        matrix = np.array(vectors, dtype=np.float32)
        if matrix.ndim != 2 or len(matrix) != len(texts):
            raise EmbedUnavailable("embedding server returned an unexpected shape")
        return normalize(matrix)

    def _embed_batch(self, batch: list[str]) -> list[list[float]]:
        body = json.dumps({"model": self.model, "input": batch, "truncate": True}).encode()
        request = urllib.request.Request(
            f"{self.endpoint}/api/embed", data=body, headers={"Content-Type": "application/json"}
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                payload = json.loads(response.read())
        except urllib.error.HTTPError as err:
            detail = err.read()[:200].decode("utf-8", "replace")
            raise EmbedUnavailable(f"embedding server answered {err.code}: {detail}") from err
        except (urllib.error.URLError, TimeoutError, OSError, json.JSONDecodeError) as err:
            raise EmbedUnavailable(f"embedding server unreachable: {err}") from err
        embeddings = payload.get("embeddings")
        if not isinstance(embeddings, list) or len(embeddings) != len(batch):
            raise EmbedUnavailable("embedding server returned the wrong number of vectors")
        return embeddings

    def reachable(self) -> bool:
        """A cheap liveness probe for status; it does not load the model."""
        try:
            with urllib.request.urlopen(f"{self.endpoint}/api/version", timeout=1.5):
                return True
        except (urllib.error.URLError, TimeoutError, OSError):
            return False
