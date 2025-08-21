from typing import List

from logging_config import get_logger

logger = get_logger(__name__)




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

    def get_handlers(self, event_type: str) -> List[BaseHandler]:
        handlers_info = self._registry.get(event_type, [])
        handlers = []
        for info in handlers_info:
            handler = info["cls"](storage=info["storage"], publisher=info["publisher"], **info["args"])
            handlers.append(handler)
        return handlers