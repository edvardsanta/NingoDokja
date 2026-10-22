"""Compatibility adapter for existing book outputs, hosted as a document plugin."""
from __future__ import annotations

import contextlib
import json
import os
import sys


def main():
    request = json.load(sys.stdin)
    if request.get("operation") != "document.process.v1" or request.get("action") not in {"classify", "summarize"}:
        raise ValueError("unsupported document processing operation")
    root = os.environ.get("DOKJA_BOOK_DIR", "").strip()
    if not root:
        raise ValueError("DOKJA_BOOK_DIR must name the installed compatibility processor")
    sys.path.insert(0, root)
    # Existing provider selection and output shape are preserved. Dependencies and
    # any model output go to stderr; stdout belongs to the plugin protocol alone.
    with contextlib.redirect_stdout(sys.stderr):
        from server import build_service
        service = build_service()
        result = getattr(service, request["action"])(request.get("payload") or {})
    json.dump({"result": result}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(type(error).__name__, file=sys.stderr)
        json.dump({"error": "document processor failed; check plugin dependencies and configuration"}, sys.stdout)
