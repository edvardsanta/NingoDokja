"""PDF text. Scanned documents have no text layer; reading them would need OCR, which is not built in."""

from __future__ import annotations

import io
import posixpath

from . import ExtractError, Extracted

MAX_PAGES = 2_000
MIN_CHARACTERS = 20


def extract(data: bytes, name: str) -> list[Extracted]:
    try:
        from pypdf import PdfReader
    except ImportError as err:  # pragma: no cover - the dependency is part of the image
        raise ExtractError("PDF support needs the pypdf package") from err

    reader = PdfReader(io.BytesIO(data))
    if reader.is_encrypted:
        try:
            if not reader.decrypt(""):
                raise ExtractError("the PDF is password protected")
        except ExtractError:
            raise
        except Exception as err:
            raise ExtractError("the PDF is password protected") from err
    if len(reader.pages) > MAX_PAGES:
        raise ExtractError(f"the PDF has more than {MAX_PAGES} pages; split it first")

    pages = [(page.extract_text() or "").strip() for page in reader.pages]
    text = "\n\n".join(page for page in pages if page)
    if len(text.strip()) < MIN_CHARACTERS:
        raise ExtractError("the PDF has no text layer (a scanned document?); OCR is not built in")

    title = ""
    try:
        title = " ".join(str((reader.metadata or {}).get("/Title") or "").split())
    except Exception:
        title = ""
    return [Extracted(title=title or posixpath.splitext(posixpath.basename(name))[0], text=text)]
