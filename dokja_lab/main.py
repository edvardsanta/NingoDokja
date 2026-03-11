import os
import signal
import sys
from flask import Flask, redirect, url_for

from config import WORKERS, RESPONDER_HANDLERS
from infra.zeromq.responder import Responder
from logging_config import get_logger
import threading
from scheduler import start_workers
from worker_routes import workers_bp
from memes_routes import memes_bp
from chat_message_routes import chat_messages_bp

logger = get_logger(__name__)
stop_event = threading.Event()
app = Flask(__name__)
app.register_blueprint(workers_bp)
app.register_blueprint(memes_bp)
app.register_blueprint(chat_messages_bp)


@app.route("/")
def index():
    return redirect(url_for("workers.index"))


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
    logger.debug("Flask initialized on port 5000")
    env = os.getenv("NINGO_ENV", "production")
    if env == "development":
        app.run(host="0.0.0.0", port=5000)
    responder_thread.join()


if __name__ == "__main__":
    main()
