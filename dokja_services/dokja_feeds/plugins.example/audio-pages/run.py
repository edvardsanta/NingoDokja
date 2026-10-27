"""A music source that reads audio pages with yt-dlp, metadata only.

`read {url}` turns a page into its track; `search {query, limit}` searches with the
yt-dlp search prefix the owner sets. A request this source does not serve is answered
`handled: false`, so the next source is asked; a request it cannot do is answered
`handled: true` with `refused`. Only a broken plugin answers an error.

Settings, in settings.json beside this script (all optional):
- "search_prefix": a yt-dlp search prefix; without it this source does not search.
- "hosts": host names this source reads; without it, it tries any page.

Nothing is downloaded and no stream address is returned: those addresses expire and
are bound to the network that asked for them, so whoever plays a track opens it again
from its page.
"""
from __future__ import annotations

import contextlib
import json
import os
import re
import sys
from urllib.parse import urlsplit

MAX_RESULTS = 5
MAX_QUERY = 200
MAX_TEXT = 300


class Refused(Exception):
    """The request's own problem, worded here, so it is safe to show."""


def main():
    request = json.load(sys.stdin)
    action = request.get("action")
    if request.get("operation") != "audio.resolve.v1" or action not in {"read", "search"}:
        raise Refused("unsupported audio resolve operation")
    payload = request.get("payload") or {}
    configured = settings()
    if action == "read":
        url = str(payload.get("url") or "").strip()
        if not is_page(url):
            raise Refused("a valid http(s) url is required")
        hosts = configured.get("hosts")
        if hosts is not None and (urlsplit(url).hostname or "").lower() not in hosts:
            return reply({"handled": False})
        target, limit = url, 1
    else:
        query = " ".join(str(payload.get("query") or "").split())
        if not query or len(query) > MAX_QUERY:
            raise Refused(f"a query of 1 to {MAX_QUERY} characters is required")
        prefix = configured.get("search_prefix")
        if not prefix:
            return reply({"handled": False})
        limit = bounded_limit(payload.get("limit"))
        target = f"{prefix}{limit}:{query}"
    # yt-dlp is imported only once this source has taken the request.
    with contextlib.redirect_stdout(sys.stderr):
        from yt_dlp import YoutubeDL
        from yt_dlp.utils import DownloadError

        options = {
            "quiet": True,
            "no_warnings": True,
            "skip_download": True,
            "noplaylist": True,
            "extract_flat": "in_playlist",
            "socket_timeout": 15,
        }
        try:
            with YoutubeDL(options) as downloader:
                info = downloader.extract_info(target, download=False) or {}
        except DownloadError as error:
            print("yt-dlp:", error, file=sys.stderr)
            info = None
    # replies go out only after stdout is back
    if info is None:
        return reply({"handled": True, "refused": "that page could not be read as audio"})
    entries = info.get("entries") if "entries" in info else [info]
    tracks = [track for track in (describe(entry) for entry in entries or [] if entry) if track]
    reply({"handled": True, "tracks": tracks[:limit]})


def reply(result: dict):
    json.dump({"result": result}, sys.stdout, ensure_ascii=False)


def settings() -> dict:
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "settings.json")
    try:
        with open(path, encoding="utf-8") as handle:
            loaded = json.load(handle)
    except FileNotFoundError:
        return {}
    except (OSError, ValueError):
        raise Refused("settings.json beside the plugin is not valid JSON") from None
    if not isinstance(loaded, dict):
        raise Refused("settings.json must hold an object")
    chosen = {}
    prefix = loaded.get("search_prefix")
    if isinstance(prefix, str) and re.fullmatch(r"[a-z]{2,20}search", prefix):
        chosen["search_prefix"] = prefix
    hosts = loaded.get("hosts")
    if isinstance(hosts, list) and all(isinstance(host, str) for host in hosts):
        chosen["hosts"] = {host.lower() for host in hosts}
    return chosen


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
        "playable": True,
        "thumbnail": thumbnail(entry),
    }


def thumbnail(entry: dict) -> str:
    """The track's picture: the one yt-dlp names, else the last (largest) of those it lists."""
    candidates = [entry.get("thumbnail")]
    listed = entry.get("thumbnails")
    if isinstance(listed, list):
        candidates += [item.get("url") for item in reversed(listed) if isinstance(item, dict)]
    for candidate in candidates:
        if isinstance(candidate, str) and is_page(candidate) and len(candidate) <= 2048:
            return candidate
    return ""


if __name__ == "__main__":
    try:
        main()
    except Refused as error:
        json.dump({"error": str(error)}, sys.stdout)
    except Exception as error:
        print(type(error).__name__, error, file=sys.stderr)
        json.dump({"error": "audio pages failed; check the plugin's yt-dlp"}, sys.stdout)
