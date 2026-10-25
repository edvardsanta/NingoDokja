"""Durable media intake cache; retains the legacy meme table and delivery flags."""
from __future__ import annotations

import json
import os
import sqlite3
import sys
from datetime import datetime, timezone
from urllib.parse import urlsplit


def valid_url(value):
    if not isinstance(value, str) or len(value) > 2048:
        return ""
    try:
        url = urlsplit(value)
        if url.scheme in {"http", "https"} and url.hostname and not url.username and not url.password:
            return value
    except ValueError:
        pass
    return ""


def dispatch(db, action, payload):
    now = datetime.now(timezone.utc).isoformat()
    if action == "ingest":
        items = payload.get("items")
        if not isinstance(items, list) or len(items) > 1000:
            raise ValueError("items must be a list of at most 1000 observations")
        added = 0
        for item in items:
            if not isinstance(item, dict):
                continue
            url = valid_url(item.get("media_url") or item.get("url"))
            if not url or item.get("kind") != "media" and not item.get("media_url"):
                continue
            tags = item.get("tags") or []
            if not isinstance(tags, list):
                tags = []
            cursor = db.execute(
                """INSERT OR IGNORE INTO meme
                (url, title, source, tags, sent_count, date_created, date_sent)
                VALUES (?, ?, ?, ?, 0, ?, NULL)""",
                (url, str(item.get("title") or "")[:300], str(item.get("source") or "")[:80],
                 ", ".join(str(tag)[:80] for tag in tags[:20]), item.get("published") or now),
            )
            added += cursor.rowcount
        return {"added": added}
    if action == "status":
        counts = dict(db.execute("SELECT sent_count, COUNT(*) FROM meme GROUP BY sent_count"))
        return {"status": "ok", "unsent_count": counts.get(0, 0), "sent_count": counts.get(1, 0), "scrapers": []}
    if action in {"list", "fetch"}:
        scope = str(payload.get("scope") or "unsent").strip().lower()
        if scope not in {"unsent", "sent"}:
            raise ValueError("scope must be unsent or sent")
        sent = int(scope == "sent") if action == "list" else 0
        limit = min(max(int(payload.get("limit") or 20), 1), 100) if action == "list" else (-1 if payload.get("limit") is None else int(payload["limit"]))
        offset = max(int(payload.get("offset") or 0), 0) if action == "list" else 0
        total = db.execute("SELECT COUNT(*) FROM meme WHERE sent_count = ?", (sent,)).fetchone()[0]
        # SQL structure is fixed; all external values are bound parameters.
        query = """SELECT url, title, source, tags, sent_count, date_created, date_sent
                   FROM meme WHERE sent_count = ? ORDER BY date_created DESC, url LIMIT ? OFFSET ?"""
        if sent:
            query = """SELECT url, title, source, tags, sent_count, date_created, date_sent
                       FROM meme WHERE sent_count = ? ORDER BY date_sent DESC, url LIMIT ? OFFSET ?"""
        fields = ("url", "title", "source", "tags", "sent_count", "date_created", "date_sent")
        rows = [dict(zip(fields, row)) for row in db.execute(query, (sent, limit, offset))]
        return {"scope": scope, "total": total, "offset": offset, "memes": rows, "count": len(rows)}
    if action in {"contains", "mark-sent"}:
        url = valid_url(payload.get("url"))
        if not url:
            raise ValueError("a valid media url is required")
        exists = db.execute("SELECT 1 FROM meme WHERE url = ?", (url,)).fetchone() is not None
        if action == "contains":
            return {"exists": exists}
        if exists:
            db.execute("UPDATE meme SET sent_count = 1, date_sent = ? WHERE url = ?", (now, url))
        return {"url": url, "marked": exists, "reason": "" if exists else "not in pool"}
    raise ValueError("unsupported media library action")


def main():
    request = json.load(sys.stdin)
    if request.get("operation") != "media.library.v1":
        raise ValueError("unsupported operation")
    path = os.environ.get("FEEDS_MEDIA_DB_FILE", "").strip()
    if not path:
        raise ValueError("FEEDS_MEDIA_DB_FILE must name the media intake database")
    with sqlite3.connect(path, timeout=15) as db:
        db.execute("""CREATE TABLE IF NOT EXISTS meme (
            url TEXT PRIMARY KEY, title TEXT, source TEXT, tags TEXT,
            sent_count INTEGER, date_created TEXT, date_sent TEXT)""")
        # Transactions serialize intake and receipt writes across plugin processes.
        db.execute("BEGIN IMMEDIATE")
        result = dispatch(db, request.get("action"), request.get("payload") or {})
        wire = json.dumps({"result": result}, ensure_ascii=False)
        if len(wire.encode("utf-8")) > 16_000_000:
            raise ValueError("media result exceeds output limit; request fewer items")
    sys.stdout.write(wire)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(type(error).__name__, file=sys.stderr)
        json.dump({"error": str(error) if isinstance(error, ValueError) else "media library operation failed"}, sys.stdout)
