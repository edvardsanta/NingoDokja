try:
    from bootstrap import ensure_repo_root
except ImportError:  # pragma: no cover
    from dokja_lab.bootstrap import ensure_repo_root

ensure_repo_root()

from dokja_domain.dokja_chat import ChatResponse
try:
    from models.ChatMessage import ChatMessage
except ImportError:  # pragma: no cover
    from dokja_lab.models.ChatMessage import ChatMessage


class SQLiteChatMessageRepository:
    def __init__(self, storage):
        self.storage = storage

    def get_by_request_hash(self, request_hash: str) -> ChatResponse | None:
        record = self.storage.get_by_id_hash(request_hash)
        if record is None:
            return None
        return self._to_domain(record)

    def mark_cached(self, record: ChatResponse) -> None:
        if not hasattr(record, "id") or record.id is None:
            stored = self.storage.get_by_id_hash(self._request_hash(record.request_message))
            if stored is None:
                return
            record.id = stored.id
        self.storage.update(record.id, {"cached": int(record.cached)})

    def save(self, response: ChatResponse) -> None:
        self.storage.add(self._to_model(response))

    def _to_domain(self, record: ChatMessage) -> ChatResponse:
        response = ChatResponse(
            id=getattr(record, "id", None),
            type=record.type,
            role=record.role,
            request_message=record.request_message,
            response_message=record.response_message,
            timestamp=record.timestamp,
            retrieval_info=record.retrieval_info,
            cached=bool(record.cached),
            emojis=list(record.emojis or []),
            tone=record.tone,
            language=record.language,
        )
        return response

    def _to_model(self, response: ChatResponse) -> ChatMessage:
        model = ChatMessage(
            type=response.type,
            role=response.role,
            request_message=response.request_message,
            response_message=response.response_message,
            timestamp=response.timestamp,
            retrieval_info=response.retrieval_info,
            cached=response.cached,
            emojis=response.emojis,
            tone=response.tone,
            language=response.language,
        )
        if hasattr(response, "id"):
            model.id = response.id
        return model

    def _request_hash(self, content: str) -> str:
        model = ChatMessage(
            type="",
            role="user",
            request_message=content,
            response_message="",
        )
        return model.id_hash
