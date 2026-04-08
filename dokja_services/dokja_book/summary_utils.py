from __future__ import annotations

from collections import Counter
import re
from typing import Any

STOPWORDS = {
    "a",
    "an",
    "and",
    "are",
    "as",
    "at",
    "by",
    "com",
    "como",
    "da",
    "das",
    "de",
    "do",
    "dos",
    "e",
    "em",
    "é",
    "for",
    "from",
    "in",
    "is",
    "na",
    "nas",
    "no",
    "nos",
    "o",
    "of",
    "on",
    "os",
    "ou",
    "para",
    "por",
    "que",
    "se",
    "the",
    "to",
    "um",
    "uma",
}


def title_from_filename(filename: Any) -> str | None:
    if not isinstance(filename, str) or not filename.strip():
        return None
    value = filename.rsplit("/", 1)[-1]
    return value.rsplit(".", 1)[0].replace("_", " ").replace("-", " ").strip() or None


def normalize_text(content: str) -> str:
    lines = [line.strip() for line in content.replace("\r\n", "\n").split("\n")]
    normalized_lines = []
    blank = False
    for line in lines:
        if not line:
            if not blank:
                normalized_lines.append("")
            blank = True
            continue
        normalized_lines.append(line)
        blank = False
    return "\n".join(normalized_lines).strip()


def sentences(text: str) -> list[str]:
    normalized = text.replace("?", ".").replace("!", ".")
    return [sentence.strip() for sentence in normalized.split(".") if sentence.strip()]


def extractive_overview(text_sentences: list[str]) -> str:
    if not text_sentences:
        return ""

    tokens_by_sentence = [_tokens(sentence) for sentence in text_sentences]
    frequencies = Counter(
        token
        for sentence_tokens in tokens_by_sentence
        for token in sentence_tokens
        if token not in STOPWORDS and len(token) > 2
    )
    if not frequencies:
        return ". ".join(text_sentences[:3]).strip() + "."

    scored: list[tuple[int, float, str]] = []
    for index, sentence in enumerate(text_sentences):
        sentence_tokens = tokens_by_sentence[index]
        if len(sentence_tokens) < 4:
            continue

        score = sum(frequencies[token] for token in sentence_tokens if token in frequencies)
        if index == 0:
            score *= 1.05
        if len(sentence_tokens) > 40:
            score *= 0.9
        scored.append((index, score, sentence.strip()))

    if not scored:
        return ". ".join(text_sentences[:3]).strip() + "."

    selected = sorted(sorted(scored, key=lambda item: item[1], reverse=True)[:3], key=lambda item: item[0])
    return ". ".join(sentence for _, _, sentence in selected).strip() + "."


def key_points(paragraphs: list[str]) -> list[str]:
    points: list[str] = []
    for paragraph in paragraphs[:5]:
        paragraph_sentences = sentences(paragraph)
        if paragraph_sentences:
            first_sentence = paragraph_sentences[0]
            points.append(first_sentence[:220].strip() + ("..." if len(first_sentence) > 220 else ""))
    return points


def _tokens(text: str) -> list[str]:
    return re.findall(r"[a-zA-ZÀ-ÿ0-9_]+", text.lower())
