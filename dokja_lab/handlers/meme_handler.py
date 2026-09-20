from datetime import datetime
from typing import Any

from handlers.base_handler import BaseHandler
from models.Meme import Meme
from utils.mapper import to_dict


class MemeHandler(BaseHandler):
    def validate_payload(self, payload: dict) -> bool:
        return True

    def handle(self, payload: dict) -> list[dict[str, Any]]:
        if not self.storage:
            return []
        memes: list[Meme] = self.storage.get_filtered(sent_count=0)
        if not memes:
            return []
        self.storage.update_many(
            [meme.url for meme in memes],
            {"sent_count": 1, "date_sent": datetime.now()},
        )
        return [to_dict(meme) for meme in memes]
