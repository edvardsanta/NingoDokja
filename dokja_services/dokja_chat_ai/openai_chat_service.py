import logging
from typing import Any

try:
    from openai import OpenAI
except ImportError:  # pragma: no cover
    OpenAI = None

logger = logging.getLogger("dokja_chat_ai.openai")


class OpenAIChatService:
    def __init__(self, api_key: str, base_url: str, model: str = "n/a"):
        if OpenAI is None:
            raise RuntimeError("openai package is required for OpenAIChatService")
        self.client = OpenAI(api_key=api_key, base_url=base_url)
        self.model = model

    def send_message(
        self, message: str, temperature: float = 0
    ) -> dict[str, list[str | None] | dict[str, Any]]:
        return self.send_messages(
            [{"role": "user", "content": message}],
            temperature=temperature,
        )

    def send_messages(
        self, messages: list[dict[str, str]], temperature: float = 0
    ) -> dict[str, list[str | None] | dict[str, Any]]:
        normalized = [
            {
                "role": str(message.get("role", "user")).strip() or "user",
                "content": str(message.get("content", "")).strip(),
            }
            for message in messages
            if str(message.get("content", "")).strip()
        ]
        if not normalized:
            raise ValueError("messages must contain at least one non-empty content entry")

        user_chars = sum(len(message["content"]) for message in normalized if message["role"] == "user")
        logger.info(
            "sending chat completion request model=%s base_url=%s message_chars=%d history_messages=%d temperature=%s",
            self.model,
            getattr(self.client, "base_url", ""),
            user_chars,
            len(normalized),
            temperature,
        )
        response = self.client.chat.completions.create(
            model=self.model,
            messages=normalized,
            temperature=temperature,
            extra_body={"include_retrieval_info": True},
        )

        contents = [choice.message.content for choice in response.choices]
        response_dict = response.to_dict()
        retrieval_info = response_dict.get("retrieval", {})
        logger.info(
            "received chat completion response choices=%d retrieval_keys=%d",
            len(contents),
            len(retrieval_info) if isinstance(retrieval_info, dict) else 0,
        )

        return {"contents": contents, "retrieval": retrieval_info}
