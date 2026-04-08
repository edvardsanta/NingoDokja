from __future__ import annotations

from dataclasses import dataclass, field
import json
from pathlib import Path
from typing import Protocol
from urllib import request as urllib_request
from typing import Any

from transformers import pipeline

BOOK_TYPE_LABELS = ["technical", "academic", "fiction", "non-fiction"]
FORMAT_LABELS = ["pdf", "epub", "mobi", "txt", "md"]


class BookClassifier(Protocol):
    def predict_format(self, payload: dict[str, Any]) -> str: ...

    def predict_book_type(self, payload: dict[str, Any]) -> str: ...


@dataclass
class LocalBookClassifier:
    model_name: str = "facebook/bart-large-mnli"
    model_cache_dir: str | None = None
    classifier: Any | None = field(default=None, repr=False)

    def __post_init__(self) -> None:
        if self.classifier is None:
            cache_dir = self.model_cache_dir or self._default_model_cache_dir()
            Path(cache_dir).mkdir(parents=True, exist_ok=True)
            self.classifier = pipeline(
                "zero-shot-classification",
                model=self.model_name,
                cache_dir=cache_dir,
            )
            self.model_cache_dir = cache_dir

    def predict_format(self, payload: dict[str, Any]) -> str:
        return self._top_label(self.classifier(self._format_signal(payload), FORMAT_LABELS, multi_label=False), FORMAT_LABELS)

    def predict_book_type(self, payload: dict[str, Any]) -> str:
        return self._top_label(
            self.classifier(self._book_type_signal(payload), BOOK_TYPE_LABELS, multi_label=False),
            BOOK_TYPE_LABELS,
        )

    def _format_signal(self, payload: dict[str, Any]) -> str:
        metadata = payload.get("metadata") or {}
        candidates = [
            f"filename: {payload.get('filename') or ''}",
            f"resource_uri: {payload.get('resource_uri') or ''}",
            f"mime_type: {payload.get('mime_type') or ''}",
            f"declared_format: {payload.get('format') or ''}",
            f"title: {payload.get('title') or ''}",
            f"metadata_category: {metadata.get('category') or ''}",
            f"content_preview: {str(payload.get('content') or '')[:400]}",
        ]
        return "\n".join(candidates)

    def _book_type_signal(self, payload: dict[str, Any]) -> str:
        metadata = payload.get("metadata") or {}
        candidates = [
            f"title: {payload.get('title') or ''}",
            f"author: {payload.get('author') or ''}",
            f"goal: {payload.get('goal') or ''}",
            f"filename: {payload.get('filename') or ''}",
            f"format: {payload.get('format') or ''}",
            f"metadata_category: {metadata.get('category') or ''}",
            f"metadata_tags: {metadata.get('tags') or ''}",
            f"content_preview: {str(payload.get('content') or '')[:1500]}",
        ]
        return "\n".join(candidates)

    def _top_label(self, result: Any, allowed_labels: list[str]) -> str:
        labels = result.get("labels") if isinstance(result, dict) else None
        if isinstance(labels, list):
            for label in labels:
                if label in allowed_labels:
                    return label
        return allowed_labels[0]

    def _default_model_cache_dir(self) -> str:
        return str(Path(__file__).resolve().parent / "models")


@dataclass
class HTTPBookClassifier:
    endpoint: str
    timeout_seconds: int = 30

    def predict_format(self, payload: dict[str, Any]) -> str:
        response = self._post("/classify/format", payload)
        return self._extract_label(response, FORMAT_LABELS)

    def predict_book_type(self, payload: dict[str, Any]) -> str:
        response = self._post("/classify/book-type", payload)
        return self._extract_label(response, BOOK_TYPE_LABELS)

    def _post(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        body = json.dumps(payload).encode("utf-8")
        req = urllib_request.Request(
            self.endpoint.rstrip("/") + path,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib_request.urlopen(req, timeout=self.timeout_seconds) as response:
            return json.loads(response.read().decode("utf-8"))

    def _extract_label(self, payload: dict[str, Any], allowed: list[str]) -> str:
        candidates = [
            payload.get("label"),
            (payload.get("result") or {}).get("label") if isinstance(payload.get("result"), dict) else None,
            payload.get("format"),
            payload.get("book_type"),
            (payload.get("result") or {}).get("format") if isinstance(payload.get("result"), dict) else None,
            (payload.get("result") or {}).get("book_type") if isinstance(payload.get("result"), dict) else None,
        ]
        for candidate in candidates:
            if isinstance(candidate, str) and candidate in allowed:
                return candidate
        raise ValueError(f"classifier response did not contain a valid label from {allowed}")
