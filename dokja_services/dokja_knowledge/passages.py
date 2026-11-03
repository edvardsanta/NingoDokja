"""Exact spans in immutable extracted page/chapter text; offsets are Unicode code points."""
from __future__ import annotations

import base64
import hashlib
import json
import posixpath

from chunker import Chunk, MAX_CHARS, OVERLAP_CHARS


def original_source(encoded: str, filename: str, units: object) -> dict:
    if not isinstance(encoded, str) or not isinstance(filename, str):
        raise ValueError("original needs a filename and base64 content")
    data = base64.b64decode(encoded, validate=True)
    fmt = posixpath.splitext(filename.lower())[1].lstrip(".")
    if fmt not in ("pdf", "epub"):
        raise ValueError("original must be PDF or EPUB")
    if not isinstance(units, (list, tuple)) or not 0 < len(units) <= 5000:
        raise ValueError("original needs ordered page/chapter units")
    checked = []
    for i, unit in enumerate(units):
        if not isinstance(unit, dict) or not isinstance(unit.get("text"), str):
            raise ValueError("invalid original unit")
        text = unit["text"]
        if "\x00" in text:
            raise ValueError("invalid original text")
        item = {"text": text, "label": str(unit.get("label", ""))[:300]}
        if fmt == "pdf":
            if type(unit.get("page")) is not int or unit["page"] != i + 1:
                raise ValueError("PDF pages must retain original order, including empty pages")
            item["page"] = i + 1
        else:
            href = unit.get("href")
            if not isinstance(href, str) or not href or len(href) > 2000 or href.startswith("/") or ".." in href.split("/") or ":" in href or "\\" in href:
                raise ValueError("invalid EPUB chapter path")
            item["href"] = href
        checked.append(item)
    file_hash = hashlib.sha256(data).hexdigest()
    version = hashlib.sha256(json.dumps([file_hash, checked], sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    return {"version": version, "file_hash": file_hash, "format": fmt,
            "filename": posixpath.basename(filename), "content_b64": encoded, "units": checked}


def source_chunks(original: dict) -> list[Chunk]:
    chunks = []
    for index, unit in enumerate(original["units"]):
        text = unit["text"]
        start = 0
        while start < len(text):
            end = min(start + MAX_CHARS, len(text))
            if end < len(text):
                boundary = text.rfind(" ", start + MAX_CHARS // 2, end)
                if boundary > start:
                    end = boundary
            if text[start:end].strip():
                locator = {"version": original["version"], "format": original["format"],
                           "unit": index, "start": start, "end": end,
                           **{k: v for k, v in unit.items() if k != "text"}}
                locator["passage_id"] = hashlib.sha256(
                    json.dumps(locator, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
                chunks.append(Chunk(len(chunks), unit["label"], text[start:end], json.dumps(locator)))
            if end == len(text):
                break
            start = max(start + 1, end - OVERLAP_CHARS)
    return chunks


def resolve(original: dict | None, locator: object) -> dict:
    if original is None:
        return {"state": "unavailable"}
    if not isinstance(locator, dict):
        return {"state": "unresolved"}
    try:
        index, start, end = (locator[k] for k in ("unit", "start", "end"))
        if any(type(n) is not int for n in (index, start, end)):
            raise ValueError()
        if locator["version"] != original["version"] or locator["format"] != original["format"] or index < 0:
            raise ValueError()
        unit = original["units"][index]
        if not 0 <= start < end <= len(unit["text"]):
            raise ValueError()
        for key in ("page", "href"):
            if key in unit and locator.get(key) != unit[key]:
                raise ValueError()
    except (KeyError, IndexError, TypeError, ValueError):
        return {"state": "unresolved"}
    return {"state": "resolved", "version": original["version"], "format": original["format"],
            "filename": original["filename"], "file_hash": original["file_hash"], "content_b64": original["content_b64"],
            "locator": locator, "unit": unit,
            "navigation": [{**{k: v for k, v in u.items() if k != "text"}, "chars": len(u["text"])} for u in original["units"]]}
