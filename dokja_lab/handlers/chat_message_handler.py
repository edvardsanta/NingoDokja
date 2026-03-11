import logging

try:
    from bootstrap import ensure_repo_root
except ImportError:  # pragma: no cover
    from dokja_lab.bootstrap import ensure_repo_root
ensure_repo_root()

try:
    from adapters.chat_message_repository import SQLiteChatMessageRepository
    from handlers.base_handler import BaseHandler
    from logging_config import get_logger
except ImportError:  # pragma: no cover
    from dokja_lab.adapters.chat_message_repository import SQLiteChatMessageRepository
    from dokja_lab.handlers.base_handler import BaseHandler
    from dokja_lab.logging_config import get_logger

from dokja_domain.dokja_chat import ChatDomain
from dokja_services.dokja_chat_ai import OpenAIChatService

logger = get_logger(__name__, level=logging.DEBUG)


class MessageHandler(BaseHandler):

    def __init__(self, storage, publisher, ningo_token, ningo_agent, model="n/a"):
        """
        Initialize the MessageHandler with storage, publisher, and Ningo API details.
        """
        super().__init__(storage, publisher)
        self.domain = ChatDomain(
            repository=SQLiteChatMessageRepository(storage),
            ai_service=OpenAIChatService(
                api_key=ningo_token,
                base_url=ningo_agent,
                model=model,
            ),
        )

    def handle(self, payload: dict):
        logger.info("Sending message to AI...")
        response = self.domain.handle_message(payload["content"])
        logger.info("AI response received.")
        return response

    def validate_payload(self, payload: dict) -> bool:
        if payload["content"] is None or not isinstance(payload["content"], str):
            return False
        return True
