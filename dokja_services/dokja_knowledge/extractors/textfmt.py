"""Plain text and Markdown."""

from __future__ import annotations

import posixpath

from . import ExtractError, Extracted


def decode_text(data: bytes, name: str = "input") -> str:
    if b"\x00" in data:
        raise ExtractError(f"{name} is not text")
    try:
        text = data.decode("utf-8-sig")
    except UnicodeDecodeError as err:
        raise ExtractError(f"{name} is not UTF-8 text") from err
    return text


def first_heading(text: str) -> str:
    """The document's own title: a leading '# ' line, and only if nothing else comes first."""
    for line in text.split("\n"):
        line = line.strip()
        if line.startswith("# "):
            return line[2:].strip()
        if line and not line.startswith("#"):
            return ""
    return ""


def extract(data: bytes, name: str) -> list[Extracted]:
    text = decode_text(data, name)
    stem = posixpath.splitext(posixpath.basename(name))[0]
    return [Extracted(title=first_heading(text) or stem, text=text)]
