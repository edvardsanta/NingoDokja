"""Adapt any owner-installed module:factory exposing scrape(max_items) to Feeds."""
from __future__ import annotations

import contextlib
import importlib
import json
import os
import sys


def main():
    with open("source.json", encoding="utf-8") as file:
        config = json.load(file)
    root = str(config.get("module_path") or os.environ.get("FEEDS_SCRAPER_PATH", "")).strip()
    if root:
        sys.path.insert(0, root)
    factory_ref = str(config.get("factory") or os.environ.get("FEEDS_SCRAPER_FACTORY", ""))
    module, separator, factory = factory_ref.partition(":")
    if not separator or not module or not factory.isidentifier():
        raise ValueError("source.json factory must name module:factory")
    with contextlib.redirect_stdout(sys.stderr):
        scraper = getattr(importlib.import_module(module), factory)(**config.get("options", {}))
        maximum = min(max(int(config.get("max_items", 50)), 1), 200)
        kind = str(config.get("kind", "media")).strip()
        items = []
        for raw in scraper.scrape(max_items=maximum):
            if not isinstance(raw, dict):
                continue
            tags = raw.get("tags") or []
            if isinstance(tags, str):
                tags = [tag.strip() for tag in tags.split(",") if tag.strip()]
            items.append({
                "id": str(raw.get("id") or ""), "title": str(raw.get("title") or ""),
                "summary": str(raw.get("summary") or ""), "url": str(raw.get("url") or ""),
                "source": str(raw.get("source") or getattr(scraper, "source_name", "")),
                "published": str(raw.get("published") or ""), "kind": kind,
                "media_url": str(raw.get("media_url") or raw.get("url") or "") if kind == "media" else "",
                "tags": tags,
            })
            if len(items) >= maximum:
                break
    json.dump({"items": items}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
