import threading

from infra.storage import BaseStorage
from logging_config import get_logger
from models.Meme import Meme
from utils.mapper import from_dict

logger = get_logger(__name__)


class MemePool:
    def __init__(self, scrapers: list, storage: BaseStorage):
        if not isinstance(storage, BaseStorage):
            raise ValueError("Storage must be an instance of BaseStorage")
        self.scrapers = scrapers
        self.storage = storage

    def _scrape_and_store(self, scraper, max_items, errors):
        try:
            memes: list[dict] = scraper.scrape(max_items=max_items)
            logger.debug(f"Scraped {len(memes)} memes from {scraper.source_name}")
            for meme_dict in memes:
                meme = from_dict(Meme, meme_dict)
                url = meme.url
                if url and not self.storage.exists(url):
                    self.storage.add(meme)
                    logger.debug(f"Stored meme: {url}")
        except Exception as err:
            logger.exception("Scraper failed for %s", getattr(scraper, "source_name", type(scraper).__name__))
            errors.append(
                f"{getattr(scraper, 'source_name', type(scraper).__name__)}: {err}"
            )

    def refresh_pool(self, max_items_per_scraper=20):
        threads = []
        errors = []
        for scraper in self.scrapers:
            logger.debug(f"Starting scraping {scraper.source_name}")
            t = threading.Thread(
                target=self._scrape_and_store,
                args=(scraper, max_items_per_scraper, errors),
            )
            t.start()
            threads.append(t)

        for t in threads:
            t.join()

        if errors:
            raise RuntimeError("meme pool refresh failed: " + "; ".join(errors))
