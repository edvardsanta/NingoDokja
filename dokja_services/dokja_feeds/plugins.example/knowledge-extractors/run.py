"""Serves Knowledge's extractors as a reading-host plugin (document.extract.v1, ADR 0006).

The host writes one JSON request on stdin, {"operation", "filename", "format", "content_b64"},
and reads one JSON object on stdout: {"documents": [{"title", "text", "source_ref", "key"}]},
the Extracted contract, or {"error": "..."} when the file cannot be read. Anything else this
program says goes to stderr, which only reaches the host log.

KNOWLEDGE_EXTRACTORS_DIR is the directory holding Knowledge's `extractors` package and
`plugins.py`. When KNOWLEDGE_PLUGINS_DIR is set, the owner's `register(registry)` plugins are
loaded as the Knowledge service loads them; list their extensions in plugin.json `formats` so
the host routes those files here.
"""

from __future__ import annotations

import base64
import binascii
import json
import os
import sys

OPERATION = "document.extract.v1"


def answer(body: dict) -> int:
    json.dump(body, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


def main() -> int:
    root = os.environ.get("KNOWLEDGE_EXTRACTORS_DIR", "").strip()
    if not root or not os.path.isdir(os.path.join(root, "extractors")):
        print("KNOWLEDGE_EXTRACTORS_DIR must name the directory of Knowledge's extractors", file=sys.stderr)
        return 2
    sys.path.insert(0, root)
    from extractors import ExtractError, default_registry

    request = json.load(sys.stdin)
    if request.get("operation") != OPERATION:
        print(f"unsupported operation {request.get('operation')!r}", file=sys.stderr)
        return 2
    try:
        data = base64.b64decode(request.get("content_b64") or "", validate=True)
    except (binascii.Error, ValueError):
        return answer({"error": "content_b64 is not valid base64"})

    registry = default_registry()
    plugins_dir = os.environ.get("KNOWLEDGE_PLUGINS_DIR", "").strip()
    if plugins_dir:
        from plugins import load_plugins

        load_plugins(registry, plugins_dir)
    try:
        documents = registry.extract(str(request.get("filename") or ""), data)
    except ExtractError as err:
        return answer({"error": str(err)})
    return answer({
        "documents": [
            {"title": doc.title, "text": doc.text, "source_ref": doc.source_ref, "key": doc.key}
            for doc in documents
        ]
    })


if __name__ == "__main__":
    sys.exit(main())
