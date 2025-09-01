"""Shared test fixtures and utilities for dokja_lab tests."""

from unittest.mock import Mock

import pytest


@pytest.fixture
def mock_logger():
    """Provide a mock logger for testing."""
    return Mock()


@pytest.fixture
def mock_storage():
    """Provide a mock storage instance."""
    return Mock()


@pytest.fixture
def mock_publisher():
    """Provide a mock publisher instance."""
    return Mock()


@pytest.fixture
def mock_subscriber():
    """Provide a mock subscriber instance."""
    subscriber = Mock()
    subscriber.receive_item.return_value = None
    subscriber.close.return_value = None
    return subscriber


@pytest.fixture
def sample_event_data():
    """Provide sample event data for testing."""
    return {
        "meme": "meme:https://example.com/meme.jpg",
        "stock": "stock:AAPL,150.00,+5.00,+3.33",
        "news": "news:Test news headline",
    }
