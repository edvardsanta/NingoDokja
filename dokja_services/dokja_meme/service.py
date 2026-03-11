from __future__ import annotations

from datetime import datetime
from logging_config import get_logger

logger = get_logger(__name__)


class MemeService:
    def __init__(self, storage, scrapers: list, worker):
        self.storage = storage
        self.scrapers = scrapers
        self.worker = worker

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

        raise ValueError(f"unsupported meme event type: {event_type}")

    def fetch_unsent(self, limit=None) -> dict:
        memes = self.storage.get_filtered(
            sent_count=0,
            order_by="date_created",
            order_dir="DESC",
            limit=limit,
        )
        logger.info("meme fetch_unsent limit=%s found=%d", limit, len(memes))

        if memes:
            self.storage.update_many(
                [meme.url for meme in memes],
                {"sent_count": 1, "date_sent": datetime.now()},
            )
            logger.info("meme fetch_unsent marked_sent count=%d", len(memes))

        return {
            "memes": [self._to_dict(meme) for meme in memes],
            "count": len(memes),
        }

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

    def status(self) -> dict:
        unsent = self.storage.get_filtered(sent_count=0)
        logger.info("meme status unsent_count=%d", len(unsent))
        return {
            "status": "ok",
            "unsent_count": len(unsent),
            "scrapers": [getattr(scraper, "source_name", type(scraper).__name__) for scraper in self.scrapers],
        }

    def _to_dict(self, meme) -> dict:
        return {
            "url": meme.url,
            "title": meme.title,
            "source": meme.source,
            "tags": meme.tags,
            "sent_count": meme.sent_count,
            "date_created": self._serialize_datetime(meme.date_created),
            "date_sent": self._serialize_datetime(meme.date_sent),
        }

    def _serialize_datetime(self, value):
        if hasattr(value, "isoformat"):
            return value.isoformat()
        return value
