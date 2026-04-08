import logging
import os

import uvicorn

from server import create_app


def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    uvicorn.run(
        create_app(),
        host=os.getenv("DOKJA_VOICE_HOST", "0.0.0.0"),
        port=int(os.getenv("DOKJA_VOICE_PORT", "8081")),
        log_level="info",
    )


if __name__ == "__main__":
    main()
