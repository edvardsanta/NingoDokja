"""Word processor documents (.docx): a zip of XML. Namespaces are matched by local name."""

from __future__ import annotations

import posixpath
import re

from . import ExtractError, Extracted, ZipBudget, local, open_zip, parse_xml

HEADING_STYLE = re.compile(r"^(?:heading|título|titulo)\s*([1-9])$", re.IGNORECASE)


def _attribute(element, name: str) -> str:
    for key, value in element.attrib.items():
        if local(key) == name:
            return value
    return ""


def _paragraph_text(paragraph) -> str:
    pieces: list[str] = []
    for node in paragraph.iter():
        tag = local(node.tag)
        if tag == "t" and node.text:
            pieces.append(node.text)
        elif tag == "tab":
            pieces.append(" ")
        elif tag in ("br", "cr"):
            pieces.append("\n")
    return "".join(pieces).strip()


def _heading_level(paragraph) -> int:
    for node in paragraph.iter():
        if local(node.tag) == "pStyle":
            style = _attribute(node, "val").replace(" ", "")
            match = HEADING_STYLE.match(style)
            if match:
                return int(match.group(1))
            if style.lower() == "title":
                return 1
    return 0


def _blocks(body) -> list[str]:
    blocks: list[str] = []
    for child in body:
        tag = local(child.tag)
        if tag == "p":
            text = _paragraph_text(child)
            if not text:
                continue
            level = _heading_level(child)
            blocks.append("#" * level + " " + text if level else text)
        elif tag == "tbl":
            for row in (node for node in child.iter() if local(node.tag) == "tr"):
                cells = []
                for cell in (node for node in row if local(node.tag) == "tc"):
                    cells.append(" ".join(filter(None, (_paragraph_text(p) for p in cell.iter() if local(p.tag) == "p"))))
                line = " | ".join(cell for cell in cells if cell)
                if line:
                    blocks.append(line)
    return blocks


def extract(data: bytes, name: str) -> list[Extracted]:
    archive = open_zip(data)
    budget = ZipBudget()
    document = parse_xml(budget.read(archive, "word/document.xml"))
    body = next((node for node in document if local(node.tag) == "body"), None)
    if body is None:
        raise ExtractError("the document has no body")
    blocks = _blocks(body)

    title = ""
    if "docProps/core.xml" in archive.namelist():
        try:
            core = parse_xml(budget.read(archive, "docProps/core.xml"))
            title = next((" ".join((n.text or "").split()) for n in core.iter() if local(n.tag) == "title"), "")
        except ExtractError:
            title = ""
    if not title:
        title = next((b.lstrip("# ") for b in blocks if b.startswith("# ")), "")
    if not title:
        title = posixpath.splitext(posixpath.basename(name))[0]
    return [Extracted(title=title, text="\n\n".join(blocks))]
