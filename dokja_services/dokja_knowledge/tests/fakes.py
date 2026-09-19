import zlib

import numpy as np

from embedder import EmbedUnavailable, normalize
from service import terms


class FakeEmbedder:
    """Bag-of-words hashing embedder: shared words mean similar vectors, no words shared
    means (nearly) orthogonal ones. Deterministic and offline; it tests the retrieval
    logic, not the quality of a real model."""

    def __init__(self, dimensions: int = 512):
        self.dimensions = dimensions
        self.calls: list[list[str]] = []
        self.down = False
        self.up = True

    def embed(self, texts):
        self.calls.append(list(texts))
        if self.down:
            raise EmbedUnavailable("embedding server unreachable: connection refused")
        matrix = np.zeros((len(texts), self.dimensions), dtype=np.float32)
        for row, text in enumerate(texts):
            for term in terms(text):
                matrix[row, zlib.crc32(term.encode()) % self.dimensions] += 1.0
        return normalize(matrix)

    def reachable(self):
        return not self.down

    @property
    def embedded_texts(self) -> int:
        return sum(len(call) for call in self.calls)
