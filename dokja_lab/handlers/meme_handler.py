from datetime import datetime

from handlers.base_handler import BaseHandler
from models.Meme import Meme
from utils.mapper import to_dict


class MemeHandler(BaseHandler):
    def validate_payload(self, payload):
        return True

    def handle(self, payload):
        memes: list[Meme] = (
            self.storage.get_filtered(sent_count=0) if self.storage else []
        )
        if memes:
            self.storage.update_many(
                [meme.url for meme in memes],
                {"sent_count": 1, "date_sent": datetime.now()},
            )
        return [to_dict(meme) for meme in memes] if memes else []
