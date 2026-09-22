"""A small client for a local Ollama embedding server (bge-m3 by default).

Each service in this project is self-contained (its Dockerfile copies only its own
directory), so this is a trimmed sibling of dokja_services/dokja_knowledge/embedder.py
rather than a shared import: dokja_meme cannot reach across into dokja_knowledge's
package without breaking that isolation. Both talk to the same Ollama server.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import cast

import numpy as np

DEFAULT_ENDPOINT = "http://127.0.0.1:11434"
DEFAULT_MODEL = "bge-m3"


class EmbedUnavailable(RuntimeError):
    """The embedding server could not produce vectors (down, model missing, bad reply)."""


def normalize(matrix: np.ndarray) -> np.ndarray:
    """L2-normalize rows, so a dot product is the cosine similarity."""
    matrix = np.asarray(matrix, dtype=np.float32)
    norms = np.linalg.norm(matrix, axis=1, keepdims=True)
    norms[norms == 0] = 1.0
    return cast(np.ndarray, matrix / norms)


class OllamaEmbedder:
    def __init__(
        self,
        endpoint: str = DEFAULT_ENDPOINT,
        model: str = DEFAULT_MODEL,
        timeout: float = 30.0,
    ):
        self.endpoint = endpoint.rstrip("/")
        self.model = model
        self.timeout = timeout

    def embed(self, texts: list[str]) -> np.ndarray:
        """Return one normalized vector per text, in order. Raises EmbedUnavailable."""
        if not texts:
            return np.zeros((0, 0), dtype=np.float32)
        body = json.dumps(
            {"model": self.model, "input": list(texts), "truncate": True}
        ).encode()
        request = urllib.request.Request(
            f"{self.endpoint}/api/embed",
            data=body,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                payload = json.loads(response.read())
        except urllib.error.HTTPError as err:
            detail = err.read()[:200].decode("utf-8", "replace")
            raise EmbedUnavailable(
                f"embedding server answered {err.code}: {detail}"
            ) from err
        except (
            urllib.error.URLError,
            TimeoutError,
            OSError,
            json.JSONDecodeError,
        ) as err:
            raise EmbedUnavailable(f"embedding server unreachable: {err}") from err

        embeddings = payload.get("embeddings")
        if not isinstance(embeddings, list) or len(embeddings) != len(texts):
            raise EmbedUnavailable(
                "embedding server returned the wrong number of vectors"
            )
        matrix = np.array(embeddings, dtype=np.float32)
        if matrix.ndim != 2 or len(matrix) != len(texts):
            raise EmbedUnavailable("embedding server returned an unexpected shape")
        return normalize(matrix)

    def reachable(self) -> bool:
        """A cheap liveness probe; it does not load the model."""
        try:
            with urllib.request.urlopen(f"{self.endpoint}/api/version", timeout=1.5):
                return True
        except (urllib.error.URLError, TimeoutError, OSError):
            return False
