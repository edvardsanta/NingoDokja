"""HTML to Markdown-ish text: headings become '#' lines so the chunker can use them."""

from __future__ import annotations

import posixpath
import re
from html.parser import HTMLParser

from . import Extracted

SKIPPED = {"script", "style", "noscript", "template", "svg", "head", "nav", "footer", "aside", "form", "iframe"}
BLOCKS = {
    "p", "div", "section", "article", "main", "header", "ul", "ol", "table", "tr", "blockquote",
    "pre", "figure", "figcaption", "dl", "dt", "dd", "hr", "br",
}
HEADINGS = {f"h{level}": level for level in range(1, 7)}
VOID = {"br", "hr", "img", "meta", "link", "input"}


class _Reader(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.title = ""
        self._skip_depth = 0
        self._in_title = False
        self._heading = 0
        self._heading_text: list[str] = []

    def handle_starttag(self, tag, attrs):
        if tag == "title":
            self._in_title = True
            return
        if tag in VOID and tag not in BLOCKS:
            return
        if tag in SKIPPED and tag != "head":
            self._skip_depth += 1
            return
        if tag == "head":
            self._skip_depth += 1
            return
        if self._skip_depth:
            return
        if tag in HEADINGS:
            self._heading = HEADINGS[tag]
            self._heading_text = []
            self.parts.append("\n\n")
        elif tag == "li":
            self.parts.append("\n- ")
        elif tag in BLOCKS or tag in ("td", "th"):
            self.parts.append(" | " if tag in ("td", "th") else "\n\n")

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False
            return
        if tag in SKIPPED or tag == "head":
            self._skip_depth = max(0, self._skip_depth - 1)
            return
        if self._skip_depth:
            return
        if tag in HEADINGS and self._heading:
            heading = " ".join("".join(self._heading_text).split())
            if heading:
                if not self.title and self._heading == 1:
                    self.title = heading
                self.parts.append("#" * self._heading + " " + heading)
            self.parts.append("\n\n")
            self._heading = 0
        elif tag in BLOCKS:
            self.parts.append("\n\n")

    def handle_data(self, data):
        if self._in_title:
            self.title = self.title or " ".join(data.split())
            return
        if self._skip_depth:
            return
        # Line breaks inside a text run are just wrapping; the block tags carry the structure.
        data = re.sub(r"\s+", " ", data)
        if self._heading:
            self._heading_text.append(data)
        else:
            self.parts.append(data)


def decode_html(data: bytes) -> str:
    if data.startswith((b"\xff\xfe", b"\xfe\xff")):
        return data.decode("utf-16", errors="replace")
    declared = re.search(rb"charset\s*=\s*[\"']?([A-Za-z0-9_-]+)", data[:2048], re.IGNORECASE)
    if declared:
        try:
            return data.decode(declared.group(1).decode("ascii"), errors="replace")
        except LookupError:
            pass
    return data.decode("utf-8-sig", errors="replace")


def html_to_text(markup: str) -> tuple[str, str]:
    """Return (title, text) for a page or a fragment."""
    reader = _Reader()
    reader.feed(markup)
    reader.close()
    text = "".join(reader.parts)
    lines = [" ".join(line.split()) if not line.startswith("#") else line.strip() for line in text.split("\n")]
    text = re.sub(r"\n{3,}", "\n\n", "\n".join(lines)).strip()
    return reader.title, text


def extract(data: bytes, name: str) -> list[Extracted]:
    title, text = html_to_text(decode_html(data))
    stem = posixpath.splitext(posixpath.basename(name))[0]
    return [Extracted(title=title or stem, text=text)]
