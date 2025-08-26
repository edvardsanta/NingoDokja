from logging_config import get_logger
from memes.pool import MemePool
from workers.base_worker import BaseWorker

logger = get_logger(__name__)


class MemeWorker(BaseWorker):
    def __init__(self, scrapers, storage):
        self.pool = MemePool(scrapers=scrapers, storage=storage)

    def run(self):
        logger.info("Refreshing meme pool...")
        self.pool.refresh_pool()
        logger.info("Meme pool refreshed.")
