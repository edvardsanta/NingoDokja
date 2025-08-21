from handlers.base_handler import BaseHandler


class MemeHandler(BaseHandler):
    def validate_payload(self, payload):
        limit = payload.get("limit")
        if not isinstance(limit, int) or not (1 <= limit <= 100):
            return False
        return True

    def handle(self, payload):
        memes = self.storage.get_filtered(sent_count=0) if self.storage else []
        return memes
