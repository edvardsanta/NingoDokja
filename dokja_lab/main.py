from config import SCRAPERS, STORAGE
from logging_config import get_logger

logger = get_logger(__name__)

import time
from memes.pool import MemePool

def main():
    pool = MemePool(scrapers=SCRAPERS, storage=STORAGE)

    while True:
        logger.info("Refreshing meme pool...")
        pool.refresh_pool()
        logger.info("Meme pool refreshed")

        meme = pool.get_next_meme()
        if meme:
            logger.info(f"Next meme: {meme['title']} ({meme['url']})")
        else:
            logger.warning("No memes available in pool")

        time.sleep(60)


if __name__ == "__main__":
    main()
