from typing import Any, Dict, Optional, Type

from handlers.base_handler import BaseHandler
from logging_config import get_logger

logger = get_logger(__name__)


class HandlerFactory:
    def __init__(self):
        self._registry = {}

    def register_handler(
        self,
        handler_name: str,
        event_type: str,
        handler_cls: Type[BaseHandler],
        storage: Optional[Any] = None,
        publisher: Optional[Any] = None,
        args: Optional[Dict[str, Any]] = None,
    ):
        """Register a handler class along with its dependencies."""
        self._registry.setdefault(event_type, []).append(
            {
                "name": handler_name,
                "cls": handler_cls,
                "storage": storage,
                "publisher": publisher,
                "args": args or {},
            }
        )

    def get_handlers(self, event_type: str) -> Dict[str, BaseHandler]:
        """
        Return a dict mapping handler_name -> handler_instance
        for the given event type.
        """
        handlers_info = self._registry.get(event_type, [])
        return {
            info["name"]: info["cls"](
                storage=info["storage"], publisher=info["publisher"], **info["args"]
            )
            for info in handlers_info
        }

    def get_handler(self, event_type: str) -> BaseHandler:
        """Return the first registered handler instance for the event type."""
        handlers = self.get_handlers(event_type)
        if not handlers:
            logger.error(f"No handler registered for event type: {event_type}")
            raise ValueError(f"No handler registered for event type: {event_type}")
        # Return first instance (arbitrary if multiple handlers)
        return next(iter(handlers.values()))
