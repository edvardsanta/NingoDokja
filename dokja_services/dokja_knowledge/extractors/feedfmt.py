"""Syndication feeds (RSS and Atom) that the user hands over, as a file or a page they name.

This only *reads* a feed once. Following a feed over time (a stored address polled on a
schedule) is deliberately not built in: that is the user's own integration.
"""

from __future__ import annotations

import posixpath

from . import ExtractError, Extracted, local, parse_xml
from .htmlfmt import html_to_text

MAX_ENTRIES = 200


def _text(node) -> str:
    return " ".join((node.text or "").split()) if node is not None else ""


def _child(entry, *names: str):
    for child in entry:
        if local(child.tag) in names:
            return child
    return None


def _link(entry) -> str:
    for child in entry:
        if local(child.tag) != "link":
            continue
        attrs = {local(k): v for k, v in child.attrib.items()}
        if attrs.get("href") and attrs.get("rel", "alternate") == "alternate":
            return attrs["href"]
        if child.text and child.text.strip():
            return child.text.strip()
    return ""


def _body(entry) -> str:
    node = _child(entry, "encoded", "content", "description", "summary")
    if node is None:
        return ""
    raw = node.text or ""
    if "<" in raw:
        return html_to_text(raw)[1]
    return raw.strip()


def extract(data: bytes, name: str) -> list[Extracted]:
    root = parse_xml(data)
    kind = local(root.tag).lower()
    if kind not in ("rss", "feed", "rdf"):
        raise ExtractError("the XML is not a syndication feed")
    entries = [node for node in root.iter() if local(node.tag) in ("item", "entry")]
    feed_title = _text(next((n for n in root.iter() if local(n.tag) == "title"), None))

    documents: list[Extracted] = []
    for entry in entries[:MAX_ENTRIES]:
        title = _text(_child(entry, "title")) or feed_title or posixpath.basename(name)
        text = _body(entry)
        if not text:
            continue
        link = _link(entry)
        key = _text(_child(entry, "guid", "id")) or link or title
        documents.append(Extracted(title=title, text=text, source_ref=link, key=key))
    return documents
