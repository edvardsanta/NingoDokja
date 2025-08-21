import hashlib
import json
from datetime import datetime
from ai.chat_agent import ChatAgent
from handlers.base_handler import BaseHandler
from models.ChatMessage import ChatMessage
from utils.mapper import to_dict


class MessageHandler(BaseHandler):
    def __init__(self, storage, api_key, base_url, model="n/a"):
        super().__init__(storage)
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
