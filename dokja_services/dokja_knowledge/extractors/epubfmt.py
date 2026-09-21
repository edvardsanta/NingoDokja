"""E-books (.epub): a zip holding an OPF package and an ordered list of XHTML chapters."""

from __future__ import annotations

import posixpath

from . import ExtractError, Extracted, ZipBudget, local, open_zip, parse_xml, safe_member_path
from .htmlfmt import decode_html, html_to_text

MAX_CHAPTERS = 5_000
CHAPTER_TYPES = ("application/xhtml+xml", "text/html")


def _package_path(archive, budget: ZipBudget) -> str:
    container = parse_xml(budget.read(archive, "META-INF/container.xml"))
    for node in container.iter():
        if local(node.tag) == "rootfile":
            for key, value in node.attrib.items():
                if local(key) == "full-path":
                    return safe_member_path("", value)
    raise ExtractError("the e-book has no package file")


def extract(data: bytes, name: str) -> list[Extracted]:
    archive = open_zip(data)
    budget = ZipBudget()
    package = _package_path(archive, budget)
    base_dir = posixpath.dirname(package)
    root = parse_xml(budget.read(archive, package))

    title = next((" ".join((n.text or "").split()) for n in root.iter() if local(n.tag) == "title" and n.text), "")
    manifest: dict[str, tuple[str, str]] = {}
    for node in root.iter():
        if local(node.tag) == "item":
            attrs = {local(key): value for key, value in node.attrib.items()}
            if attrs.get("id") and attrs.get("href"):
                manifest[attrs["id"]] = (attrs["href"], attrs.get("media-type", ""))

    order = [
        {local(k): v for k, v in node.attrib.items()}.get("idref", "")
        for node in root.iter()
        if local(node.tag) == "itemref"
    ]
    if len(order) > MAX_CHAPTERS:
        raise ExtractError("the e-book has too many chapters")

    chapters: list[str] = []
    for item_id in order:
        href, media_type = manifest.get(item_id, ("", ""))
        if not href or media_type not in CHAPTER_TYPES:
            continue
        member = safe_member_path(base_dir, href)
        if member not in archive.namelist():
            continue
        _, text = html_to_text(decode_html(budget.read(archive, member)))
        if text:
            chapters.append(text)
    if not title:
        title = posixpath.splitext(posixpath.basename(name))[0]
    return [Extracted(title=title, text="\n\n".join(chapters))]
