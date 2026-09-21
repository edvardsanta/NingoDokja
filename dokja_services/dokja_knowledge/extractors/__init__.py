"""Turn files and fetched pages into plain text the knowledge base can chunk.

Only generic format mechanisms live in the repository. Anything specific to one site or
provider is an out-of-tree plugin (see plugins.py), so nothing here names a website.
"""

from __future__ import annotations

import io
import posixpath
import re
import xml.etree.ElementTree as ElementTree
import zipfile
from dataclasses import dataclass
from typing import Callable

MAX_UNPACKED_BYTES = 40_000_000
MAX_ZIP_MEMBERS = 5_000


class ExtractError(ValueError):
    """The input could not be turned into text (unsupported, damaged, empty or unsafe)."""


@dataclass(frozen=True)
class Extracted:
    title: str
    text: str
    source_ref: str = ""  # where an entry came from, when the format says so
    key: str = ""  # stable id of an entry inside a multi-document file


Extractor = Callable[[bytes, str], "list[Extracted]"]
Fetcher = Callable[[str], "tuple[bytes, str]"]  # ref -> (data, file name)

# Content types a fetched page may carry, mapped to the extension that picks its extractor.
CONTENT_TYPES = {
    "text/plain": ".txt",
    "text/markdown": ".md",
    "text/html": ".html",
    "application/xhtml+xml": ".html",
    "application/pdf": ".pdf",
    "application/epub+zip": ".epub",
    "application/rss+xml": ".xml",
    "application/atom+xml": ".xml",
    "application/xml": ".xml",
    "text/xml": ".xml",
}


class Registry:
    """Extractors by file extension and fetchers by reference scheme."""

    def __init__(self) -> None:
        self._extractors: dict[str, Extractor] = {}
        self._fetchers: dict[str, Fetcher] = {}

    def add_extractor(self, extensions, extractor: Extractor) -> None:
        names = [extensions] if isinstance(extensions, str) else list(extensions)
        for extension in names:
            extension = extension.lower()
            self._extractors[extension if extension.startswith(".") else "." + extension] = extractor

    def add_fetcher(self, scheme: str, fetcher: Fetcher) -> None:
        scheme = scheme.strip().lower()
        if scheme in ("http", "https"):
            raise ValueError("http and https are handled by the built-in fetcher")
        if not re.fullmatch(r"[a-z][a-z0-9+.-]{0,31}", scheme):
            raise ValueError(f"invalid scheme {scheme!r}")
        self._fetchers[scheme] = fetcher

    def fetcher(self, scheme: str) -> Fetcher | None:
        return self._fetchers.get(scheme.lower())

    @property
    def schemes(self) -> list[str]:
        return sorted(self._fetchers)

    @property
    def extensions(self) -> list[str]:
        return sorted(self._extractors)

    def extract(self, name: str, data: bytes, content_type: str = "") -> list[Extracted]:
        extension = posixpath.splitext(name.lower())[1]
        if extension not in self._extractors:
            base = content_type.split(";")[0].strip().lower()
            if base.endswith("wordprocessingml.document"):
                extension = ".docx"
            else:
                extension = CONTENT_TYPES.get(base, extension)
        extractor = self._extractors.get(extension)
        if extractor is None:
            raise ExtractError(
                f"unsupported file type {extension or '(none)'!r}; supported: {', '.join(self.extensions)}"
            )
        try:
            documents = extractor(data, name)
        except ExtractError:
            raise
        except Exception as err:  # a damaged or hostile file must never take the service down
            raise ExtractError(f"could not read {name!r}: {type(err).__name__}") from err
        documents = [doc for doc in documents if doc.text.strip()]
        if not documents:
            raise ExtractError(f"{name!r} has no text to store")
        return documents


# ---- helpers shared by the zip and XML based formats -----------------------------------


def local(tag: str) -> str:
    """Tag or attribute name without its namespace, so no format URIs are needed here."""
    return tag.rsplit("}", 1)[-1]


def parse_xml(data: bytes) -> ElementTree.Element:
    """Parse untrusted XML. Entity declarations are refused (entity expansion attacks)."""
    if re.search(rb"<!ENTITY", data, re.IGNORECASE):
        raise ExtractError("XML with entity declarations is not accepted")
    try:
        return ElementTree.fromstring(data)
    except ElementTree.ParseError as err:
        raise ExtractError("damaged XML") from err


def open_zip(data: bytes) -> zipfile.ZipFile:
    try:
        archive = zipfile.ZipFile(io.BytesIO(data))
    except zipfile.BadZipFile as err:
        raise ExtractError("not a valid archive") from err
    if len(archive.infolist()) > MAX_ZIP_MEMBERS:
        raise ExtractError("archive has too many files")
    return archive


class ZipBudget:
    """Caps how much a zip may unpack to, however its headers describe it."""

    def __init__(self, limit: int | None = None) -> None:
        self.remaining = MAX_UNPACKED_BYTES if limit is None else limit

    def read(self, archive: zipfile.ZipFile, member: str) -> bytes:
        try:
            info = archive.getinfo(member)
        except KeyError as err:
            raise ExtractError(f"archive is missing {member}") from err
        if info.file_size > self.remaining:
            raise ExtractError("archive unpacks to more than the allowed size")
        with archive.open(info) as handle:
            content = handle.read(self.remaining + 1)
        if len(content) > self.remaining:
            raise ExtractError("archive unpacks to more than the allowed size")
        self.remaining -= len(content)
        return content


def safe_member_path(base_dir: str, href: str) -> str:
    """Resolve a reference found inside an archive without letting it climb out."""
    href = href.split("#", 1)[0]
    path = posixpath.normpath(posixpath.join(base_dir, href))
    if path == ".." or path.startswith("../") or path.startswith("/"):
        raise ExtractError("archive references a path outside itself")
    return path


def default_registry() -> Registry:
    from . import docxfmt, epubfmt, feedfmt, htmlfmt, pdffmt, textfmt

    registry = Registry()
    registry.add_extractor([".txt", ".md", ".markdown", ".rst"], textfmt.extract)
    registry.add_extractor([".html", ".htm", ".xhtml"], htmlfmt.extract)
    registry.add_extractor(".docx", docxfmt.extract)
    registry.add_extractor(".epub", epubfmt.extract)
    registry.add_extractor(".pdf", pdffmt.extract)
    registry.add_extractor([".xml", ".rss", ".atom"], feedfmt.extract)
    return registry
