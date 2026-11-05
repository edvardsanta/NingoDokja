"""Fixed PDF text reader. No URLs, plugins, OCR, scripts or embedded attachments."""

import io
import json
import resource
import sys


def main():
    resource.setrlimit(resource.RLIMIT_AS, (512 << 20, 512 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (8, 8))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    from pypdf import PdfReader

    limit = int(sys.argv[2])
    data = sys.stdin.buffer.read(limit + 1)
    if len(data) > limit or not data.startswith(b"%PDF-"):
        raise ValueError("unreadable PDF")
    reader = PdfReader(io.BytesIO(data))
    if reader.is_encrypted:
        raise ValueError("encrypted PDF")
    start = int(sys.argv[1]) - 1
    count = len(reader.pages)
    if start < 0 or start >= count:
        raise ValueError("missing page")
    remaining = 80000
    pages = []
    partial = start > 0
    end = min(count, start + 500)
    for index in range(start, end):
        text = (reader.pages[index].extract_text() or "").replace("\x00", "").strip()
        if not text:
            partial = True
            continue
        if len(text) > remaining:
            text = text[:remaining]
            partial = True
        pages.append({"page": index + 1, "text": text})
        remaining -= len(text)
        if remaining == 0:
            partial = partial or index + 1 < count
            break
    partial = partial or end < count
    if not pages:
        raise ValueError("no extractable text")
    title = str((reader.metadata or {}).get("/Title", ""))[:300]
    json.dump({"title": title, "partial": partial, "pages": pages}, sys.stdout, ensure_ascii=True)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.exit(1)
