import time
from datetime import datetime

from apscheduler.events import EVENT_JOB_EXECUTED, EVENT_JOB_ERROR
from apscheduler.schedulers.background import BackgroundScheduler
from logging_config import get_logger

logger = get_logger(__name__)

worker_status = {}
scheduler_instance: BackgroundScheduler = None
worker_configs = {}


def start_workers(workers_config):
    global scheduler_instance
    scheduler = BackgroundScheduler()
    scheduler_instance = scheduler

    for config in workers_config:
        worker_class = config["worker_class"]
        init_args = config.get("init_args", [])
        init_kwargs = config.get("init_kwargs", {})
        scheduler_params = config["scheduler_params"].copy()
        worker_instance = worker_class(*init_args, **init_kwargs)
        job = scheduler.add_job(worker_instance.run, **scheduler_params)
        logger.info(
            f"Resgistrada {worker_class.__name__} com a configuração {scheduler_params}"
        )
        worker_status[job.name] = {
            "status": "scheduled",
            "timestamp": None,
            "exception": None,
            "next_run": None,
            "trigger": scheduler_params.get("trigger", "interval"),
        }
        # Save config for later initialization and execute_now
        worker_configs[job.name] = {
            "worker_class": worker_class,
            "init_args": init_args,
            "init_kwargs": init_kwargs,
            "scheduler_params": scheduler_params,
        }

    def listener(event):
        job_listener = scheduler.get_job(event.job_id)
        worker_name = job_listener.name
        now = datetime.now().strftime("%d-%m-%Y %H:%M:%S")  # Formato legível
        next_run_ts = (
            job_listener.next_run_time.timestamp()
            if job_listener.next_run_time
            else None
        )
        next_run_str = (
            datetime.fromtimestamp(next_run_ts).strftime("%d-%m-%Y %H:%M:%S")
            if next_run_ts
            else None
        )

        if event.exception:
            worker_status[worker_name].update(
                {
                    "status": "error",
                    "timestamp": now,
                    "exception": str(event.exception),
                    "next_run": next_run_str,
                }
            )
        else:
            worker_status[worker_name].update(
                {
                    "status": "success",
                    "timestamp": now,
                    "exception": None,
                    "next_run": next_run_str,
                }
            )

    scheduler.add_listener(listener, EVENT_JOB_EXECUTED | EVENT_JOB_ERROR)
    scheduler.start()
    logger.info("Worker scheduler started")

    return scheduler


def get_scheduler():
    return scheduler_instance
