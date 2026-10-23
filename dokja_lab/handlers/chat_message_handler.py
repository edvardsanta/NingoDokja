import logging
from typing import Any, Protocol

try:
    from bootstrap import ensure_repo_root
except ImportError:  # pragma: no cover
    from dokja_lab.bootstrap import ensure_repo_root  # type: ignore[no-redef]
ensure_repo_root()

try:
    from adapters.chat_message_repository import SQLiteChatMessageRepository
    from handlers.base_handler import BaseHandler
    from logging_config import get_logger
except ImportError:  # pragma: no cover
    from dokja_lab.adapters.chat_message_repository import SQLiteChatMessageRepository  # type: ignore[no-redef]
    from dokja_lab.handlers.base_handler import BaseHandler  # type: ignore[no-redef]
    from dokja_lab.logging_config import get_logger  # type: ignore[no-redef]

from dokja_domain.dokja_chat import ChatDomain

logger = get_logger(__name__, level=logging.DEBUG)


class ChatProvider(Protocol):
    def send_message(self, message: str) -> Any: ...


class MessageHandler(BaseHandler):

    def __init__(
        self,
        storage: Any,
        publisher: Any,
        chat_provider: ChatProvider,
    ) -> None:
        """
        Initialize the MessageHandler with storage, publisher, and Ningo API details.
        """
        super().__init__(storage, publisher)
        self.domain = ChatDomain(
            repository=SQLiteChatMessageRepository(storage),
            ai_service=chat_provider,
        )

    def handle(self, payload: dict) -> Any:
        logger.info("Sending message to AI...")
        response = self.domain.handle_message(payload["content"])
        logger.info("AI response received.")
        return response

    def validate_payload(self, payload: dict) -> bool:
        if payload["content"] is None or not isinstance(payload["content"], str):
            return False
        return True
