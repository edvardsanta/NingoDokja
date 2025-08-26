"""Tests for the logging configuration module."""

import logging
from unittest.mock import Mock, patch

import pytest

from logging_config import get_logger


class TestLoggingConfig:
    """Test cases for logging configuration."""

    def test_get_logger_basic(self):
        """Test basic logger creation."""
        logger = get_logger("test_logger")

        assert logger.name == "test_logger"
        assert logger.level == logging.INFO
        assert len(logger.handlers) >= 1

    def test_get_logger_with_level(self):
        """Test logger creation with custom level."""
        logger = get_logger("test_logger_debug", level=logging.DEBUG)

        assert logger.level == logging.DEBUG

    def test_get_logger_cached(self):
        """Test that logger instances are cached."""
        logger1 = get_logger("cached_logger")
        logger2 = get_logger("cached_logger")

        assert logger1 is logger2

    @patch("logging_config.logging.FileHandler")
    def test_get_logger_with_file(self, mock_file_handler):
        """Test logger creation with file output."""
        mock_handler = Mock()
        mock_file_handler.return_value = mock_handler

        logger = get_logger("file_logger", log_file="/tmp/test.log")

        mock_file_handler.assert_called_once_with("/tmp/test.log")
        mock_handler.setLevel.assert_called_with(logging.INFO)
        mock_handler.setFormatter.assert_called_once()

    def test_logger_formatting(self):
        """Test that logger has proper formatting."""
        logger = get_logger("format_test")

        # Check that handlers have formatters
        for handler in logger.handlers:
            assert handler.formatter is not None
            assert "%(asctime)s" in handler.formatter._fmt
            assert "%(levelname)s" in handler.formatter._fmt
            assert "%(name)s" in handler.formatter._fmt
            assert "%(message)s" in handler.formatter._fmt
