import hashlib
import json
from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Protocol


@dataclass
class ChatResponse:
    type: str
    role: str
    request_message: str
    response_message: str
    id: int | None = None
    timestamp: datetime = field(default_factory=datetime.now)
    retrieval_info: dict[str, Any] | None = None
    cached: bool = False
    emojis: list[str] = field(default_factory=list)
    tone: str = ""
    language: str = ""


class ChatAIPort(Protocol):
    def send_message(
        self, message: str, temperature: float = 0
    ) -> dict[str, Any]: ...


class ChatMessageRepository(Protocol):
    def get_by_request_hash(self, request_hash: str) -> ChatResponse | None: ...

    def mark_cached(self, record: ChatResponse) -> None: ...

    def save(self, response: ChatResponse) -> None: ...


class ChatDomain:
    def __init__(self, repository: ChatMessageRepository, ai_service: ChatAIPort):
        self.repository = repository
        self.ai_service = ai_service

    def handle_message(self, content: str) -> str:
        if not isinstance(content, str) or not content.strip():
            raise ValueError("content must be a non-empty string")

        request_hash = hashlib.sha256(content.encode("utf-8")).hexdigest()
        cached = self.repository.get_by_request_hash(request_hash)
        if cached is not None and cached.request_message == content:
            cached.cached = True
            self.repository.mark_cached(cached)
            return cached.response_message

        result = self.ai_service.send_message(content)
        contents = result.get("contents") or []
        if not contents:
            raise ValueError("AI service returned no content")

        response = self._build_response(contents[0], content, result.get("retrieval"))
        self.repository.save(response)
        return response.response_message

    def _build_response(
        self, raw_content: str | dict[str, Any] | None, request_message: str, retrieval: Any
    ) -> ChatResponse:
        if isinstance(raw_content, dict):
            data = raw_content
        elif isinstance(raw_content, str):
            data = json.loads(raw_content)
        else:
            raise ValueError("AI response content must be a dict or JSON string")

        ningo = data.get("ningo_response", {})
        role = "user" if "user_message" in data else "ai"

        return ChatResponse(
            type=ningo.get("type", ""),
            role=role,
            request_message=data.get("user_message", request_message),
            response_message=ningo.get("text", ""),
            timestamp=datetime.now(),
            retrieval_info=retrieval,
            cached=data.get("cached", False),
            emojis=ningo.get("emojis", []),
            tone=ningo.get("tone", ""),
            language=ningo.get("language", ""),
        )
