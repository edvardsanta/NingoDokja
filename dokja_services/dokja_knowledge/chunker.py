"""Splits a document into chunks that are small enough to embed and to quote."""

from __future__ import annotations

import re
from dataclasses import dataclass

MAX_CHARS = 1200
OVERLAP_CHARS = 150

_HEADING = re.compile(r"^(#{1,6})\s+(.*\S)\s*$")
_SENTENCE_END = re.compile(r"(?<=[.!?])\s+")


@dataclass(frozen=True)
class Chunk:
    position: int
    heading: str  # "Section > Subsection" the chunk sits under, or ""
    text: str


def chunk_text(body: str, max_chars: int = MAX_CHARS, overlap: int = OVERLAP_CHARS) -> list[Chunk]:
    """Pack paragraphs into chunks of at most max_chars, never across a heading.

    Consecutive chunks under the same heading share a short tail so a sentence cut at
    a boundary still appears whole in one of them.
    """
    paragraphs = _paragraphs(body)
    chunks: list[Chunk] = []
    current: list[str] = []
    current_heading = ""

    def flush() -> None:
        text = "\n\n".join(current).strip()
        if text:
            chunks.append(Chunk(len(chunks), current_heading, text))

    for heading, paragraph in paragraphs:
        for piece in _fit(paragraph, max_chars):
            size = sum(len(part) for part in current) + 2 * len(current)
            if current and (heading != current_heading or size + len(piece) > max_chars):
                same_section = heading == current_heading
                tail = _tail(current[-1], overlap) if same_section else ""
                flush()
                current = [tail] if tail else []
            current_heading = heading
            current.append(piece)
    flush()
    return chunks


def _paragraphs(body: str) -> list[tuple[str, str]]:
    """(heading path, paragraph) pairs in document order."""
    path: list[tuple[int, str]] = []
    out: list[tuple[str, str]] = []
    buffer: list[str] = []

    def emit() -> None:
        text = " ".join(line.strip() for line in buffer).strip()
        buffer.clear()
        if text:
            out.append((" > ".join(name for _, name in path), text))

    for line in body.replace("\r\n", "\n").split("\n"):
        match = _HEADING.match(line)
        if match:
            emit()
            level = len(match.group(1))
            while path and path[-1][0] >= level:
                path.pop()
            path.append((level, match.group(2)))
        elif not line.strip():
            emit()
        else:
            buffer.append(line)
    emit()
    return out


def _fit(paragraph: str, max_chars: int) -> list[str]:
    """Split a paragraph that is too long, by sentence and then by word."""
    if len(paragraph) <= max_chars:
        return [paragraph]
    pieces: list[str] = []
    current = ""
    for sentence in _SENTENCE_END.split(paragraph):
        for part in _hard_split(sentence, max_chars):
            if current and len(current) + 1 + len(part) > max_chars:
                pieces.append(current)
                current = part
            else:
                current = f"{current} {part}".strip()
    if current:
        pieces.append(current)
    return pieces


def _hard_split(text: str, max_chars: int) -> list[str]:
    if len(text) <= max_chars:
        return [text]
    parts: list[str] = []
    current = ""
    for word in text.split():
        if current and len(current) + 1 + len(word) > max_chars:
            parts.append(current)
            current = word
        else:
            current = f"{current} {word}".strip()
    if current:
        parts.append(current)
    return parts


def _tail(text: str, overlap: int) -> str:
    if overlap <= 0:
        return ""
    if len(text) <= overlap:
        return text
    cut = text[-overlap:]
    space = cut.find(" ")
    return cut[space + 1 :].strip() if space >= 0 else ""
