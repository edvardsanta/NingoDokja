import logging
import signal

from server import KnowledgeServiceServer


def stop(_signum, _frame):
    # The container engine stops a module with SIGTERM. As PID 1 the process ignores it
    # unless it handles it, so a stop hung until the engine gave up; leaving cleanly
    # also keeps exit code 0, so a deliberate stop is not mistaken for a crash.
    raise SystemExit(0)


def main():
    signal.signal(signal.SIGTERM, stop)
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    KnowledgeServiceServer().start()


if __name__ == "__main__":
    main()
