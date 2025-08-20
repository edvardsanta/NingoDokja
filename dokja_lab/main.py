import signal
import sys

from config import HANDLER_FACTORY
from logging_config import get_logger
import threading
from infra.subscriber import BaseSubscriber

logger = get_logger(__name__)
stop_event = threading.Event()

def event_dispatcher(subscriber: BaseSubscriber):
    try:
        while not stop_event.is_set():
            message = subscriber.receive_item() # Simulated message for testing
            if not message:
                continue

            prefix, _, data = message.partition(":")
            handlers = HANDLER_FACTORY.get_handlers(prefix)

            if not handlers:
                logger.warning("No handlers registered for event type '%s'", prefix)
                continue

            for handler in handlers:
                try:
                    handler(data)
                except Exception as e:
                    logger.exception(
                        "Error in handler %s for event '%s'",
                        getattr(handler, "__class__", type(handler)).__name__,
                        prefix,
                    )
    except Exception as e:
        logger.exception("Fatal error in dispatcher loop: %s", e)
    finally:
        subscriber.close()


def main():

    event_thread = threading.Thread(
        target=event_dispatcher, args=(None,), daemon=True
    )

    def shutdown(signum, frame):
        logger.info("Shutting down dispatcher...")
        stop_event.set()
        event_thread.join()
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    event_thread.start()
    event_thread.join()


if __name__ == "__main__":
    main()