import logging
import sys

def get_logger(name=None, level=logging.INFO, log_file=None):
    """
    Returns a centralized logger instance.
    - name: module name (usually __name__)
    - level: logging level (DEBUG, INFO, WARNING, ERROR)
    - log_file: optional file path to log to disk
    """
    logger = logging.getLogger(name)
    if logger.handlers:
        # Logger already configured
        return logger

    logger.setLevel(level)
    formatter = logging.Formatter(
        "%(asctime)s [%(levelname)s] [%(name)s] %(message)s"
    )

    # Console handler
    ch = logging.StreamHandler(sys.stdout)
    ch.setLevel(level)
    ch.setFormatter(formatter)
    logger.addHandler(ch)

    # Optional file handler
    if log_file:
        fh = logging.FileHandler(log_file)
        fh.setLevel(level)
        fh.setFormatter(formatter)
        logger.addHandler(fh)

    return logger
