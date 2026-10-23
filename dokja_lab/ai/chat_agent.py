"""Compatibility wrapper around an injected chat provider."""

from __future__ import annotations

from typing import Any


class ChatAgent:
    """Forward messages to an explicitly supplied provider adapter."""

    def __init__(self, provider: Any):
        self.provider = provider

    def send_message(self, message: str, temperature: float = 0) -> Any:
        return self.provider.send_message(message, temperature=temperature)

    def send_messages(
        self, messages: list[dict[str, str]], temperature: float = 0
    ) -> Any:
        return self.provider.send_messages(messages, temperature=temperature)
