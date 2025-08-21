from apscheduler.schedulers.background import BackgroundScheduler
from logging_config import get_logger

logger = get_logger(__name__)


def start_workers(workers_config):
    scheduler = BackgroundScheduler()

    for config in workers_config:
        worker = config.pop("worker")
        scheduler.add_job(worker.run, **config)
        logger.info(f"Registered {worker.__class__.__name__} with config {config}")

    scheduler.start()
    logger.info("Worker scheduler started")

    return scheduler
