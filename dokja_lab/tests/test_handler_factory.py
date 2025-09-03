"""Tests for the handler factory module."""

from unittest.mock import Mock

import pytest

from handler_factory import HandlerFactory
from handlers.base_handler import BaseHandler


class TestHandler(BaseHandler):
    """Test handler for testing purposes."""

    def __init__(self, storage=None, publisher=None):
        super().__init__(storage, publisher)
        self.call_count = 0

    def validate_payload(self, payload: dict) -> bool:
        """Validate the payload."""
        return True

    def handle(self, payload: dict):
        """Handle the payload."""
        self.call_count += 1
        return f"processed: {payload}"

    def __call__(self, data):
        """Allow handler to be called directly with data."""
        payload = {"data": data} if not isinstance(data, dict) else data
        return self.handle(payload)


class TestHandlerFactory:
    """Test cases for HandlerFactory."""

    def test_init(self):
        """Test factory initialization."""
        factory = HandlerFactory()
        assert factory._registry == {}

    def test_register_handler(self):
        """Test handler registration."""
        factory = HandlerFactory()
        storage = Mock()
        publisher = Mock()

        factory.register_handler(
            "test_handler",
            "test_event",
            TestHandler,
            storage=storage,
            publisher=publisher,
            args={"custom_arg": "value"},
        )

        assert "test_event" in factory._registry
        assert len(factory._registry["test_event"]) == 1

        handler_info = factory._registry["test_event"][0]
        assert handler_info["name"] == "test_handler"
        assert handler_info["cls"] == TestHandler
        assert handler_info["storage"] == storage
        assert handler_info["publisher"] == publisher
        assert handler_info["args"] == {"custom_arg": "value"}

    def test_get_handlers(self, mock_storage, mock_publisher):
        """Test getting handlers for an event type."""
        factory = HandlerFactory()

        factory.register_handler(
            "handler1",
            "test_event",
            TestHandler,
            storage=mock_storage,
            publisher=mock_publisher,
        )

        factory.register_handler(
            "handler2",
            "test_event",
            TestHandler,
            storage=mock_storage,
            publisher=mock_publisher,
        )

        handlers = factory.get_handlers("test_event")

        assert len(handlers) == 2
        assert "handler1" in handlers
        assert "handler2" in handlers
        assert isinstance(handlers["handler1"], TestHandler)
        assert isinstance(handlers["handler2"], TestHandler)

    def test_get_handlers_empty(self):
        """Test getting handlers for non-existent event type."""
        factory = HandlerFactory()
        handlers = factory.get_handlers("nonexistent")
        assert handlers == {}

    def test_get_handler(self, mock_storage, mock_publisher):
        """Test getting single handler for an event type."""
        factory = HandlerFactory()

        factory.register_handler(
            "test_handler",
            "test_event",
            TestHandler,
            storage=mock_storage,
            publisher=mock_publisher,
        )

        handler = factory.get_handler("test_event")
        assert isinstance(handler, TestHandler)
        assert handler.storage == mock_storage
        assert handler.publisher == mock_publisher

    def test_get_handler_not_found(self):
        """Test getting handler for non-existent event type."""
        factory = HandlerFactory()

        with pytest.raises(
            ValueError, match="No handler registered for event type: nonexistent"
        ):
            factory.get_handler("nonexistent")
