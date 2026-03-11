import logging
import os

import uvicorn

from server import create_app


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    uvicorn.run(
        create_app(),
        host=os.getenv("CHAT_AI_SERVICE_HOST", "0.0.0.0"),
        port=int(os.getenv("CHAT_AI_SERVICE_PORT", "8080")),
        log_level="info",
    )


if __name__ == "__main__":
    main()
