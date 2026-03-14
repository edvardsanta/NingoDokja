import json

import pytest

from dokja_domain.dokja_chat.domain import ChatDomain, ChatResponse


class FakeRepository:
    def __init__(self, cached: ChatResponse | None = None):
        self.cached = cached
        self.marked_cached = None
        self.saved = None

    def get_by_request_hash(self, request_hash: str) -> ChatResponse | None:
        return self.cached

    def mark_cached(self, record: ChatResponse) -> None:
        self.marked_cached = record

    def save(self, response: ChatResponse) -> None:
        self.saved = response


class FakeAIService:
    def __init__(self, result: dict):
        self.result = result
        self.messages = []

    def send_message(self, message: str, temperature: float = 0) -> dict:
        self.messages.append((message, temperature))
        return self.result


def test_chat_domain_returns_cached_response():
    cached = ChatResponse(
        type="fun_response",
        role="ai",
        request_message="oi",
        response_message="resposta em cache",
    )
    repository = FakeRepository(cached=cached)
    service = FakeAIService(result={})
    domain = ChatDomain(repository, service)

    result = domain.handle_message("oi")

    assert result == "resposta em cache"
    assert repository.marked_cached is cached
    assert service.messages == []


def test_chat_domain_calls_ai_and_saves_response():
    repository = FakeRepository()
    service = FakeAIService(
        result={
            "contents": [
                json.dumps(
                    {
                        "user_message": "oi",
                        "ningo_response": {
                            "text": "ola",
                            "type": "fun_response",
                            "emojis": ["🙂"],
                            "tone": "leve",
                            "language": "pt",
                        },
                    }
                )
            ],
            "retrieval": {"source": "memory"},
        }
    )
    domain = ChatDomain(repository, service)

    result = domain.handle_message("oi")

    assert result == "ola"
    assert repository.saved is not None
    assert repository.saved.request_message == "oi"
    assert repository.saved.response_message == "ola"
    assert repository.saved.retrieval_info == {"source": "memory"}


def test_chat_domain_rejects_empty_content():
    domain = ChatDomain(FakeRepository(), FakeAIService({"contents": []}))

    with pytest.raises(ValueError, match="content must be a non-empty string"):
        domain.handle_message("  ")
