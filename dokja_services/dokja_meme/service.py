from __future__ import annotations

from datetime import datetime
from types import SimpleNamespace
from logging_config import get_logger
from memes.safety import ScreenUnavailable

logger = get_logger(__name__)

LIST_SCOPES = {"unsent": 0, "sent": 1}
DEFAULT_LIST_LIMIT = 20
MAX_LIST_LIMIT = 100


class MemeService:
    def __init__(self, storage, scrapers: list, worker, screen=None):
        self.storage = storage
        self.scrapers = scrapers
        self.worker = worker
        self.screen = screen

    def dispatch(self, event: dict) -> dict:
        event_type = (event.get("type") or event.get("event_type") or "").strip()
        payload = event.get("payload") or {}
        logger.info(
            "meme dispatch event_type=%s payload_keys=%d",
            event_type,
            len(payload),
        )

        if event_type == "meme.fetch":
            limit = payload.get("limit")
            return self.fetch_unsent(limit=limit)
        if event_type == "meme.pool.refresh":
            max_items = payload.get("max_items_per_scraper", 20)
            return self.refresh_pool(max_items_per_scraper=max_items)
        if event_type == "meme.status":
            return self.status()
        if event_type == "meme.screen":
            return self.screen_url(payload.get("url"), payload.get("caption"))
        if event_type == "meme.list":
            return self.list_memes(
                scope=payload.get("scope"), limit=payload.get("limit"), offset=payload.get("offset")
            )
        if event_type == "meme.mark_sent":
            return self.mark_sent(payload.get("url"))

        raise ValueError(f"unsupported meme event type: {event_type}")

    def fetch_unsent(self, limit=None) -> dict:
        limit = int(limit) if limit is not None else None
        memes = self.storage.get_filtered(
            sent_count=0,
            order_by="date_created",
            order_dir="DESC",
            limit=limit,
        )
        logger.info("meme fetch_unsent limit=%s found=%d", limit, len(memes))

        # Screening only labels a meme. Whether an unsafe one is delivered is up to the
        # caller, which knows the destination (some channels take everything).
        entries = [self._to_dict(meme, nsfw=self._classify(meme)) for meme in memes]

        if memes:
            self.storage.update_many(
                [meme.url for meme in memes],
                {"sent_count": 1, "date_sent": datetime.now()},
            )
            logger.info("meme fetch_unsent marked_sent count=%d", len(memes))

        return {"memes": entries, "count": len(entries)}

    def _classify(self, meme) -> dict:
        if self.screen is None:
            return {"safe": True, "reason": "filter disabled"}
        try:
            verdict = self.screen.check(meme)
        except ScreenUnavailable as err:
            # Fail closed for whoever asked for safe memes, without blocking the rest.
            logger.error("meme screen unavailable url=%s error=%s", meme.url, err)
            return {"safe": False, "reason": f"screen unavailable: {err}"}
        if not verdict.safe:
            logger.info("meme flagged url=%s reason=%s", meme.url, verdict.reason)
        return {"safe": verdict.safe, "reason": verdict.reason}

    def refresh_pool(self, max_items_per_scraper: int = 20) -> dict:
        logger.info(
            "meme refresh_pool max_items_per_scraper=%d scrapers=%d",
            max_items_per_scraper,
            len(self.scrapers),
        )
        self.worker.pool.refresh_pool(max_items_per_scraper=max_items_per_scraper)
        return {
            "scrapers": [getattr(scraper, "source_name", type(scraper).__name__) for scraper in self.scrapers],
            "max_items_per_scraper": max_items_per_scraper,
            "status": "refreshed",
        }

    def list_memes(self, scope=None, limit=None, offset=None) -> dict:
        """Browse the pool without consuming it (unlike fetch_unsent)."""
        scope = str(scope or "unsent").strip().lower()
        if scope not in LIST_SCOPES:
            raise ValueError(f"meme.list scope must be one of {sorted(LIST_SCOPES)}, got {scope!r}")
        limit = min(max(int(limit or DEFAULT_LIST_LIMIT), 1), MAX_LIST_LIMIT)
        offset = max(int(offset or 0), 0)

        order_by = "date_created" if scope == "unsent" else "date_sent"
        rows = self.storage.get_filtered(
            sent_count=LIST_SCOPES[scope], order_by=order_by, order_dir="DESC"
        )
        page = rows[offset : offset + limit]
        return {
            "scope": scope,
            "total": len(rows),
            "offset": offset,
            "memes": [self._to_dict(meme) for meme in page],
            "count": len(page),
        }

    def mark_sent(self, url) -> dict:
        url = str(url or "").strip()
        if not url:
            raise ValueError("meme.mark_sent requires a url")
        if not self.storage.exists(url):
            return {"url": url, "marked": False, "reason": "not in pool"}
        self.storage.update_many([url], {"sent_count": 1, "date_sent": datetime.now()})
        logger.info("meme mark_sent url=%s", url)
        return {"url": url, "marked": True}

    def screen_url(self, url, caption=None) -> dict:
        url = str(url or "").strip()
        if not url.lower().startswith(("http://", "https://")):
            raise ValueError("meme.screen requires an http(s) url")
        if self.screen is None:
            raise ValueError("nsfw filter is disabled (MEME_NSFW_FILTER=off)")

        screening = self.screen.inspect(
            SimpleNamespace(url=url, title=str(caption or ""), tags="")
        )
        logger.info("meme screen url=%s safe=%s", url, screening.verdict.safe)
        return {
            "url": url,
            "safe": screening.verdict.safe,
            "reason": screening.verdict.reason,
            "text": screening.text,
            "detections": [
                {"class": d.get("class"), "score": round(float(d.get("score", 0)), 3)}
                for d in screening.detections
            ],
            "thresholds": {
                "exposed": self.screen.threshold,
                "strict": self.screen.strict_threshold if self.screen.strict else None,
            },
        }

    def status(self) -> dict:
        unsent = self.storage.get_filtered(sent_count=0)
        sent = self.storage.get_filtered(sent_count=1)
        logger.info("meme status unsent_count=%d sent_count=%d", len(unsent), len(sent))
        return {
            "status": "ok",
            "unsent_count": len(unsent),
            "sent_count": len(sent),
            "scrapers": [getattr(scraper, "source_name", type(scraper).__name__) for scraper in self.scrapers],
        }

    def _to_dict(self, meme, nsfw: dict | None = None) -> dict:
        entry = {
            "url": meme.url,
            "title": meme.title,
            "source": meme.source,
            "tags": meme.tags,
            "sent_count": meme.sent_count,
            "date_created": self._serialize_datetime(meme.date_created),
            "date_sent": self._serialize_datetime(meme.date_sent),
        }
        if nsfw is not None:
            entry["nsfw"] = nsfw
        return entry

    def _serialize_datetime(self, value):
        if hasattr(value, "isoformat"):
            return value.isoformat()
        return value
