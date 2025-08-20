import hashlib
import json
import time
from abc import ABC, abstractmethod
from datetime import datetime
from typing import TypeVar

from ai.chat_agent import ChatAgent
from infra.storage import BaseStorage
from logging_config import get_logger
from memes.pool import MemePool
from models.ChatMessage import ChatMessage
from utils.mapper import to_dict

logger = get_logger(__name__)

T = TypeVar('T')
class BaseHandler(ABC):
    def __init__(self, storage : BaseStorage[T], publisher):
        self.storage = storage
        self.publisher = publisher

    def __call__(self, event_data):
        logger.info(f"Handling {self.__class__.__name__} event: {event_data}")
        self.persist(event_data)
        self.publisher.publish_item(event_data)

    @abstractmethod
    def persist(self, event_data):
        pass


class MemeHandler(BaseHandler):
    def __init__(self, storage, publisher, scrapers=None):
        super().__init__(storage, publisher)
        self.storage = storage
        self.publisher = publisher
        self.pool = MemePool(scrapers=scrapers, storage=storage)

    def persist(self, event_data):
        if event_data == "random":
            self.pool.get_next_meme()
            return
        try:
            while True:
                logger.info("Refreshing meme pool...")
                self.pool.refresh_pool()
                logger.info("Meme pool refreshed")

                meme = self.pool.get_next_meme()
                if meme:
                    logger.info(f"Next meme: {meme['title']} ({meme['url']})")
                    self.publisher.publish_item(meme)
                else:
                    logger.warning("No memes available in pool")

                time.sleep(60)
        finally:
            self.publisher.close()


class FinanceHandler(BaseHandler):
    def persist(self, event_data):
        self.storage.save_finance_record(event_data)


class MessageHandler(BaseHandler):
    def __init__(self, storage, publisher, api_key, base_url, model="n/a"):
        super().__init__(storage, publisher)
        self.chat_client = ChatAgent(api_key=api_key, base_url=base_url, model=model)

    from datetime import datetime
    from typing import Optional, Dict, List

    def _dict_to_chatmessage(self, data: dict) -> ChatMessage:
        """
        Convert a dictionary (from AI response or other source) into a ChatMessage instance.
        """
        ningo = data.get("ningo_response", {})

        return ChatMessage(
            type=ningo.get("type", ""),
            role="user" if "user_message" in data else "ai",
            request_message=data.get("user_message", ""),
            response_message=ningo.get("text", ""),
            timestamp=datetime.now(),
            retrieval_info=data.get("retrieval_info"),
            cached=data.get("cached", False),
            emojis=ningo.get("emojis", []),
            tone=ningo.get("tone", ""),
            language=ningo.get("language", "")
        )

    def persist(self, event_data):
        """
        Send the message to Ningo AI and store the response in storage.
        """
        request_hash = hashlib.sha256(str(event_data).encode("utf-8")).hexdigest()

        cached_message = self.storage.get_by_id_hash(request_hash)
        if cached_message and cached_message.request_message == event_data:
            cached_message.cached = True
            self.storage.update(cached_message.id, to_dict(cached_message))
            self.publisher.publish_item({"role": "ai", "content": cached_message.response_message})
            return cached_message

        result = self.chat_client.send_message(event_data)
        # Store AI contents in storage
        for content in result["contents"]:
            # Convert JSON to ChatMessage instance
            if isinstance(content, dict):
                data = content
            else:
                data = json.loads(content)
            message = self._dict_to_chatmessage(data)

            # Store message
            self.storage.add(message)

            # Publish at the same time
            self.publisher.publish_item({"role": "ai", "content": content})


class HandlerFactory:
    def __init__(self):
        self._registry = {}

    def register_handler(self, event_type: str, handler_cls, storage=None, publisher=None, args=None):
        """Register a handler class along with its dependencies."""
        if event_type not in self._registry:
            self._registry[event_type] = []
        self._registry[event_type].append({
            "cls": handler_cls,
            "storage": storage,
            "publisher": publisher,
            "args": args or {}
        })

    def get_handlers(self, event_type: str):
        handlers_info = self._registry.get(event_type, [])
        handlers = []
        for info in handlers_info:
            handler = info["cls"](storage=info["storage"], publisher=info["publisher"], **info["args"])
            handlers.append(handler)
        return handlers