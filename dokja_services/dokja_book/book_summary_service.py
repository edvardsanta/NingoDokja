from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from classifier import BookClassifier, LocalBookClassifier
from extractors import extract_resource_content
from overview_generator import ExtractiveOverviewGenerator, OverviewGenerator
from summary_utils import key_points, normalize_text, sentences, title_from_filename


@dataclass
class BookSummaryService:
    default_language: str = "pt-BR"
    default_goal: str = "study"
    classifier: BookClassifier | Any = None
    overview_generator: OverviewGenerator | Any = None

    def __post_init__(self) -> None:
        if self.classifier is None:
            self.classifier = LocalBookClassifier()
        if self.overview_generator is None:
            self.overview_generator = ExtractiveOverviewGenerator()

    def summarize(self, payload: dict[str, Any]) -> dict[str, Any]:
        resolved_payload = dict(payload)
        content = self._ensure_content(resolved_payload)
        classification = self.classify({**resolved_payload, "content": content})
        if not content:
            raise ValueError(
                "book content is required for summarize; use classify first when only the resource is available"
            )

        normalized = self._normalize_text(content)
        paragraphs = [segment for segment in normalized.split("\n\n") if segment.strip()]
        sentences = self._sentences(normalized)

        return {
            "status": "ok",
            "title": payload.get("title") or title_from_filename(payload.get("filename")),
            "classification": classification,
            "summary": {
                "overview": self.overview_generator.generate(content, sentences),
                "key_points": key_points(paragraphs),
                "recommended_focus": classification["context_strategy"]["focus"],
                "compression": {
                    "input_chars": len(content),
                    "normalized_chars": len(normalized),
                    "paragraphs": len(paragraphs),
                    "sentences": len(sentences),
                },
            },
        }

    def classify(self, payload: dict[str, Any]) -> dict[str, Any]:
        resolved_payload = dict(payload)
        file_format = self.classifier.predict_format(resolved_payload)
        preview = self._content_preview_for_classification(resolved_payload, file_format)
        if preview and not resolved_payload.get("content"):
            resolved_payload["content"] = preview
        book_type = self.classifier.predict_book_type(resolved_payload)
        language = (payload.get("language") or self.default_language).strip() or self.default_language
        goal = (payload.get("goal") or self.default_goal).strip() or self.default_goal

        return {
            "status": "ok",
            "format": file_format,
            "book_type": book_type,
            "language": language,
            "goal": goal,
            "resource_strategy": self._resource_strategy(file_format),
            "context_strategy": self._context_strategy(book_type, goal),
            "agent_route": self._agent_route(book_type),
        }

    def _ensure_content(self, payload: dict[str, Any]) -> str:
        content = str(payload.get("content") or "").strip()
        if content:
            return content

        file_format = self.classifier.predict_format(payload)
        extracted = extract_resource_content(payload, file_format)
        payload["content"] = extracted
        return extracted

    def _content_preview_for_classification(self, payload: dict[str, Any], file_format: str) -> str:
        content = str(payload.get("content") or "").strip()
        if content:
            return content[:1500]

        try:
            extracted = extract_resource_content(payload, file_format)
        except ValueError:
            return ""
        return extracted[:1500]

    def _resource_strategy(self, file_format: str) -> dict[str, Any]:
        strategies = {
            "pdf": {"ingestion": "layout-aware extraction", "unit": "page+section"},
            "epub": {"ingestion": "chapter-structured extraction", "unit": "chapter+subsection"},
            "mobi": {"ingestion": "convert then chapter extraction", "unit": "chapter"},
            "txt": {"ingestion": "plain text segmentation", "unit": "paragraph+heading"},
            "md": {"ingestion": "markdown-aware segmentation", "unit": "heading+paragraph"},
        }
        return strategies.get(file_format, {"ingestion": "generic binary classification first", "unit": "chunk"})

    def _context_strategy(self, book_type: str, goal: str) -> dict[str, Any]:
        focus_map = {
            "technical": ["concepts", "algorithms", "examples", "terminology"],
            "academic": ["thesis", "method", "evidence", "conclusions"],
            "fiction": ["plot", "characters", "themes", "chapter arcs"],
            "non-fiction": ["arguments", "timeline", "entities", "takeaways"],
            "general": ["core ideas", "structure", "insights"],
        }
        compression = {
            "study": "hierarchical outline + retained definitions",
            "review": "compact chapter synopsis + references",
            "casual": "high-level narrative summary",
        }
        return {
            "focus": focus_map.get(book_type, focus_map["general"]),
            "compression_mode": compression.get(goal, "hierarchical outline + salient quotes"),
            "max_context_windows": 12,
        }

    def _agent_route(self, book_type: str) -> str:
        return f"book-{book_type}-summarizer"

    def _normalize_text(self, content: str) -> str:
        return normalize_text(content)

    def _sentences(self, text: str) -> list[str]:
        return sentences(text)
