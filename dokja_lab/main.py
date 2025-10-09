import signal
import sys
from flask import Flask

from config import WORKERS, RESPONDER_HANDLERS
from infra.zeromq.responder import Responder
from logging_config import get_logger
import threading
from scheduler import start_workers
from worker_routes import workers_bp
from memes_routes import memes_bp

logger = get_logger(__name__)
stop_event = threading.Event()


def main():
    scheduler = start_workers(WORKERS)

    responder_thread = threading.Thread(
        target=Responder(handlers=RESPONDER_HANDLERS).start
    )

    def shutdown(signum, frame):
        logger.info("Shutting down dispatcher...")
        stop_event.set()
        responder_thread.join()
        scheduler.shutdown()
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)
    responder_thread.start()
    app = Flask(__name__)
    app.register_blueprint(workers_bp)
    app.register_blueprint(memes_bp)
    logger.debug("Flask initialized on port 5000")
    app.run(host="0.0.0.0", port=5000)
    responder_thread.join()


if __name__ == "__main__":
    main()
