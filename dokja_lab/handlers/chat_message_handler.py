import hashlib
import json
import logging
from datetime import datetime

from ai.chat_agent import ChatAgent
from handlers.base_handler import BaseHandler
from logging_config import get_logger
from models.ChatMessage import ChatMessage
from utils.mapper import to_dict

logger = get_logger(__name__, level=logging.DEBUG)
class MessageHandler(BaseHandler):

    def __init__(self, storage, publisher, ningo_token, ningo_agent, model="n/a"):
        """
        Initialize the MessageHandler with storage, publisher, and Ningo API details.
        """
        super().__init__(storage, publisher)
        self.chat_client = ChatAgent(api_key=ningo_token, base_url=ningo_agent, model=model)

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
            language=ningo.get("language", ""),
        )

    def handle(self, payload: dict):

        payload: str = payload["content"]
        request_hash = hashlib.sha256(str(payload).encode("utf-8")).hexdigest()

        cached_message = self.storage.get_by_id_hash(request_hash)
        if cached_message and cached_message.request_message == payload:
            cached_message.cached = True
            self.storage.update(cached_message.id, to_dict(cached_message))
            return cached_message.response_message

        logger.debug("Message hash mismatch")
        logger.info("Sending message to AI...")
        result = self.chat_client.send_message(payload)
        logger.info("AI response received.")
        # Store AI contents in storage

        logger.debug(f"AI response contents: {result['contents']}")
        for content in result["contents"]:
            # Convert JSON to ChatMessage instance
            if isinstance(content, dict):
                data = content
            else:
                data = json.loads(content)
            message = self._dict_to_chatmessage(data)
            logger.debug(f"Storing message: {message.request_message}: {message.response_message} with hash {message.id_hash}")
            # Store message
            self.storage.add(message)
            logger.info("Message stored in database.")
            return message.response_message

    def validate_payload(self, payload: dict) -> bool:
        if payload["content"] is None or not isinstance(payload["content"], str):
            return False
        return True
