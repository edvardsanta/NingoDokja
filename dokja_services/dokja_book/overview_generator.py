from __future__ import annotations

from dataclasses import dataclass, field
import json
from pathlib import Path
from typing import Any, Protocol
from urllib import request as urllib_request

from summary_utils import extractive_overview, normalize_text


class OverviewGenerator(Protocol):
    def generate(self, content: str, text_sentences: list[str]) -> str: ...


@dataclass
class ExtractiveOverviewGenerator:
    def generate(self, content: str, text_sentences: list[str]) -> str:
        return extractive_overview(text_sentences)


@dataclass
class TransformersOverviewGenerator:
    model_name: str = "sshleifer/distilbart-cnn-12-6"
    model_cache_dir: str | None = None
    model: Any | None = field(default=None, repr=False)
    tokenizer: Any | None = field(default=None, repr=False)

    def __post_init__(self) -> None:
        if self.model is None or self.tokenizer is None:
            from transformers import AutoModelForSeq2SeqLM, AutoTokenizer

            cache_dir = self.model_cache_dir or self._default_model_cache_dir()
            Path(cache_dir).mkdir(parents=True, exist_ok=True)
            self.tokenizer = AutoTokenizer.from_pretrained(self.model_name, cache_dir=cache_dir)
            self.model = AutoModelForSeq2SeqLM.from_pretrained(self.model_name, cache_dir=cache_dir)
            self.model_cache_dir = cache_dir

    def generate(self, content: str, text_sentences: list[str]) -> str:
        normalized = normalize_text(content)
        if not normalized:
            return ""

        try:
            snippet = normalized[:4000]
            inputs = self.tokenizer(
                snippet,
                return_tensors="pt",
                max_length=1024,
                truncation=True,
            )
            output_ids = self.model.generate(
                **inputs,
                max_length=220,
                min_length=60,
                do_sample=False,
                num_beams=4,
                length_penalty=1.0,
                early_stopping=True,
            )
            summary = self.tokenizer.decode(output_ids[0], skip_special_tokens=True).strip()
            if summary:
                return summary
        except Exception:
            pass
        return extractive_overview(text_sentences)

    def _default_model_cache_dir(self) -> str:
        return str(Path(__file__).resolve().parent / "models")


@dataclass
class ExternalOverviewGenerator:
    endpoint: str
    timeout_seconds: int = 60

    def generate(self, content: str, text_sentences: list[str]) -> str:
        body = json.dumps(
            {
                "content": content,
                "sentences": text_sentences,
            }
        ).encode("utf-8")
        req = urllib_request.Request(
            self.endpoint.rstrip("/") + "/overview",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib_request.urlopen(req, timeout=self.timeout_seconds) as response:
            payload = json.loads(response.read().decode("utf-8"))

        candidates = [
            payload.get("summary"),
            payload.get("overview"),
            (payload.get("result") or {}).get("summary") if isinstance(payload.get("result"), dict) else None,
            (payload.get("result") or {}).get("overview") if isinstance(payload.get("result"), dict) else None,
        ]
        for candidate in candidates:
            if isinstance(candidate, str) and candidate.strip():
                return candidate.strip()
        raise ValueError("external overview response did not contain summary text")
