"""Turn a link or a search into audio tracks with yt-dlp, reading metadata only.

Nothing is downloaded and no stream address is returned: those addresses expire and are bound to
the network that asked for them, so whoever plays a track resolves it again from its page.
"""
from __future__ import annotations

import contextlib
import json
import sys
from urllib.parse import urlsplit

MAX_RESULTS = 5
MAX_QUERY = 200
MAX_TEXT = 300


class Refused(Exception):
    """The request's own problem, worded here, so it is safe to show."""


def main():
    request = json.load(sys.stdin)
    if request.get("operation") != "audio.resolve.v1" or request.get("action") != "resolve":
        raise Refused("unsupported audio resolve operation")
    payload = request.get("payload") or {}
    url = str(payload.get("url") or "").strip()
    query = " ".join(str(payload.get("query") or "").split())
    if bool(url) == bool(query):
        raise Refused("give exactly one of url or query")
    limit = bounded_limit(payload.get("limit"))
    if url:
        if not is_page(url):
            raise Refused("a valid http(s) url is required")
        target = url
    else:
        if len(query) > MAX_QUERY:
            raise Refused(f"the query is longer than {MAX_QUERY} characters")
        target = f"ytsearch{limit}:{query}"
    # yt-dlp and its extractors may print; stdout carries only the reply.
    with contextlib.redirect_stdout(sys.stderr):
        from yt_dlp import YoutubeDL

        options = {
            "quiet": True,
            "no_warnings": True,
            "skip_download": True,
            "noplaylist": True,
            "extract_flat": "in_playlist",
            "socket_timeout": 15,
        }
        with YoutubeDL(options) as downloader:
            info = downloader.extract_info(target, download=False) or {}
    entries = info.get("entries") if "entries" in info else [info]
    tracks = [track for track in (describe(entry) for entry in entries or [] if entry) if track]
    json.dump({"result": {"tracks": tracks[:limit]}}, sys.stdout, ensure_ascii=False)


def bounded_limit(value) -> int:
    try:
        limit = int(value or 1)
    except (TypeError, ValueError):
        raise Refused("limit must be a number") from None
    return min(max(limit, 1), MAX_RESULTS)


def is_page(url: str) -> bool:
    parsed = urlsplit(url)
    return parsed.scheme in {"http", "https"} and bool(parsed.hostname) and not parsed.username and not parsed.password


def describe(entry: dict) -> dict | None:
    page = str(entry.get("webpage_url") or entry.get("url") or "")
    identifier = str(entry.get("id") or "")
    if not identifier or not is_page(page):
        return None
    duration = entry.get("duration")
    return {
        "id": identifier[:MAX_TEXT],
        "source": str(entry.get("ie_key") or entry.get("extractor_key") or entry.get("extractor") or "")[:MAX_TEXT],
        "url": page,
        "title": str(entry.get("title") or "")[:MAX_TEXT],
        "uploader": str(entry.get("uploader") or entry.get("channel") or "")[:MAX_TEXT],
        "duration_s": int(duration) if isinstance(duration, (int, float)) else None,
        "live": bool(entry.get("is_live") or entry.get("live_status") == "is_live"),
    }


if __name__ == "__main__":
    try:
        main()
    except Refused as error:
        json.dump({"error": str(error)}, sys.stdout)
    except Exception as error:
        print(type(error).__name__, error, file=sys.stderr)
        json.dump({"error": "audio resolve failed; check the link and the plugin's yt-dlp"}, sys.stdout)
