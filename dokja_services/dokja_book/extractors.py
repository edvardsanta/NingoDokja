from __future__ import annotations

import base64
from contextlib import contextmanager
from pathlib import Path
import shutil
import tempfile
from typing import Any


def resolve_resource_path(payload: dict[str, Any]) -> Path | None:
    for candidate in (payload.get("resource_uri"), payload.get("filename")):
        if not isinstance(candidate, str) or not candidate.strip():
            continue
        value = candidate.strip()
        if value.startswith("file://"):
            value = value[7:]
        path = Path(value).expanduser()
        if path.exists() and path.is_file():
            return path
    return None


def extract_resource_content(payload: dict[str, Any], file_format: str) -> str:
    with materialize_resource(payload, file_format) as resource_path:
        match file_format:
            case "pdf" | "epub":
                return extract_with_pymupdf(resource_path)
            case "mobi":
                return extract_mobi(resource_path)
            case "md":
                return extract_markdown(resource_path)
            case "txt":
                return resource_path.read_text(encoding="utf-8", errors="ignore")
            case _:
                raise ValueError(f"unsupported book format for extraction: {file_format}")


@contextmanager
def materialize_resource(payload: dict[str, Any], file_format: str):
    resource_path = resolve_resource_path(payload)
    temp_path: Path | None = None

    if resource_path is None:
        resource_bytes_b64 = payload.get("resource_bytes_b64")
        if isinstance(resource_bytes_b64, str) and resource_bytes_b64.strip():
            suffix = "." + file_format if file_format else ""
            with tempfile.NamedTemporaryFile(delete=False, suffix=suffix) as handle:
                handle.write(base64.b64decode(resource_bytes_b64))
                temp_path = Path(handle.name)
            resource_path = temp_path

    if resource_path is None:
        raise ValueError("book content or a readable local resource is required")

    try:
        yield resource_path
    finally:
        if temp_path is not None:
            temp_path.unlink(missing_ok=True)


def extract_with_pymupdf(resource_path: Path) -> str:
    try:
        import fitz
    except ImportError as exc:
        raise ValueError("PyMuPDF is required for PDF and EPUB extraction") from exc

    texts: list[str] = []
    document = fitz.open(resource_path)
    try:
        for page in document:
            page_text = page.get_text("text").strip()
            if page_text:
                texts.append(page_text)
    finally:
        document.close()
    return "\n\n".join(texts).strip()


def extract_mobi(resource_path: Path) -> str:
    try:
        import mobi
    except ImportError as exc:
        raise ValueError("mobi is required for MOBI extraction") from exc

    temp_root: str | None = None
    extracted_path: Path | None = None
    try:
        extracted = mobi.extract(str(resource_path))
        if isinstance(extracted, tuple):
            if len(extracted) >= 1 and extracted[0]:
                temp_root = str(extracted[0])
            if len(extracted) >= 2 and extracted[1]:
                extracted_path = Path(extracted[1])
        elif isinstance(extracted, str):
            extracted_path = Path(extracted)

        if extracted_path is None:
            raise ValueError("mobi extractor did not return an extracted file path")

        if extracted_path.is_dir():
            candidates = sorted(extracted_path.rglob("*.html")) + sorted(extracted_path.rglob("*.htm")) + sorted(
                extracted_path.rglob("*.txt")
            )
        else:
            candidates = [extracted_path]

        parts: list[str] = []
        for candidate in candidates:
            if candidate.suffix.lower() in {".html", ".htm"}:
                parts.append(html_to_text(candidate.read_text(encoding="utf-8", errors="ignore")))
            else:
                parts.append(candidate.read_text(encoding="utf-8", errors="ignore"))
        return "\n\n".join(part for part in parts if part.strip()).strip()
    finally:
        if temp_root:
            shutil.rmtree(temp_root, ignore_errors=True)


def extract_markdown(resource_path: Path) -> str:
    try:
        from markdown_it import MarkdownIt
    except ImportError as exc:
        raise ValueError("markdown-it-py is required for Markdown extraction") from exc

    markdown = resource_path.read_text(encoding="utf-8", errors="ignore")
    parser = MarkdownIt()
    tokens = parser.parse(markdown)

    parts: list[str] = []
    for token in tokens:
        if token.type == "inline" and token.children:
            for child in token.children:
                if child.type in {"text", "code_inline"} and child.content.strip():
                    parts.append(child.content.strip())
        elif token.type == "fence" and token.content.strip():
            parts.append(token.content.strip())
    return "\n".join(parts).strip()


def html_to_text(html: str) -> str:
    try:
        import fitz
    except ImportError:
        return html

    with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8") as handle:
        handle.write(html)
        temp_path = Path(handle.name)
    try:
        return extract_with_pymupdf(temp_path)
    finally:
        temp_path.unlink(missing_ok=True)
