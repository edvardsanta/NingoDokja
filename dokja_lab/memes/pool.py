import threading
import random
from typing import List, Dict

from infra.storage import BaseStorage
from logging_config import get_logger

logger = get_logger(__name__)

class MemePool:
    def __init__(self, scrapers: list, storage: BaseStorage):
        if not isinstance(storage, BaseStorage):
            raise ValueError("Storage must be an instance of BaseStorage")
        self.scrapers = scrapers
        self.storage = storage

    def _scrape_and_store(self, scraper, max_items):
        memes : list[dict] = scraper.scrape(max_items=max_items)
        logger.debug(f"Scraped {len(memes)} memes")
        for meme in memes:
            url = meme.get("url")
            if url and not self.storage.exists_meme(url):
                self.storage.add_meme(meme)
                logger.debug(f"Stored meme: {url}")

    def refresh_pool(self, max_items_per_scraper=20):
        threads = []
        for scraper in self.scrapers:
            logger.debug(f"Starting scraping {scraper.source_name}")
            t = threading.Thread(target=self._scrape_and_store, args=(scraper, max_items_per_scraper))
            t.start()
            threads.append(t)

        for t in threads:
            t.join()


    def get_next_meme(self) -> Dict|None:
        logger.debug("Getting next meme")
        all_memes = self.storage.get_all_memes()
        if not all_memes:
            return None
        meme_bytes = random.choice(all_memes)
        return meme_bytes
