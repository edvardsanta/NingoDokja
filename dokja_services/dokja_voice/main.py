import argparse
from copy import deepcopy
import os
from pathlib import Path

import uvicorn
from uvicorn.config import LOGGING_CONFIG


def main() -> None:
    parser = argparse.ArgumentParser(description="Run Dokja speech providers")
    parser.add_argument("--reload", action="store_true", help="Reload Python source in development")
    args = parser.parse_args()
    logging_config = deepcopy(LOGGING_CONFIG)
    logging_config["formatters"]["application"] = {
        "format": "%(asctime)s %(levelname)s %(name)s %(message)s"
    }
    logging_config["handlers"]["application"] = {
        "class": "logging.StreamHandler",
        "formatter": "application",
        "stream": "ext://sys.stderr",
    }
    # Uvicorn applies this configuration to each reload worker as well.
    logging_config["root"] = {"handlers": ["application"], "level": "INFO"}
    options = {
        "host": os.getenv("DOKJA_VOICE_HOST", "0.0.0.0"),
        "port": int(os.getenv("DOKJA_VOICE_PORT", "8081")),
        "log_level": "info",
        "log_config": logging_config,
    }
    if args.reload:
        uvicorn.run(
            "server:create_app",
            factory=True,
            reload=True,
            reload_dirs=[str(Path(__file__).resolve().parent)],
            **options,
        )
    else:
        from server import create_app

        uvicorn.run(create_app(), **options)


if __name__ == "__main__":
    main()
