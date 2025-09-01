import signal
import sys
import threading

from logging_config import get_logger

logger = get_logger(__name__)
stop_event = threading.Event()


def main():
    def shutdown(signum, frame):
        logger.info("Shutting down dispatcher...")
        stop_event.set()
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)



if __name__ == "__main__":
    main()
