"""Validation boundary for scoped, durable macro context."""

import json
from urllib.parse import urlsplit


def _key(payload, name):
    value = payload.get(name)
    if not isinstance(value, str) or not value or len(value) > 1024:
        raise ValueError(f"{name} is required and bounded")
    return value


def _address(raw):
    if not isinstance(raw, str) or len(raw) > 2048:
        raise ValueError("invalid context reference")
    value = urlsplit(raw)
    if value.scheme not in {"http", "https"} or not value.hostname or value.username:
        raise ValueError("invalid context reference")
    return value


def dispatch(store, operation, payload):
    if getattr(store, "backend", "") != "postgres":
        raise ValueError("durable macro context requires PostgreSQL Knowledge storage")
    scope = _key(payload, "scope")
    if operation == "recall":
        query = payload.get("query", "")
        if not isinstance(query, str) or len(query) > 2000:
            raise ValueError("invalid context query")
        return store.discussion_recall(scope, query)
    session = _key(payload, "session")
    if operation == "load":
        return store.discussion_load(scope, session)
    if operation == "save":
        documents = payload.get("documents")
        if not isinstance(documents, list) or len(documents) > 4:
            raise ValueError("context accepts at most four documents")
        if len(json.dumps(documents).encode()) > 4 << 20:
            raise ValueError("context documents exceed storage bounds")
        for document in documents:
            base = _address(document["url"])
            passages = document.get("passages")
            if not isinstance(passages, list) or not 1 <= len(passages) <= 2000:
                raise ValueError("invalid context passages")
            if len(document.get("title", "")) > 300:
                raise ValueError("invalid context title")
            total = 0
            for passage in passages:
                address = _address(passage["url"])
                if address._replace(fragment="") != base._replace(fragment=""):
                    raise ValueError("unrelated context reference")
                text = passage["text"]
                if not isinstance(text, str) or len(text) > 2400:
                    raise ValueError("invalid context passage")
                total += len(text)
                if total > 80000 or len(passage.get("label", "")) > 200:
                    raise ValueError("context text exceeds bounds")
        return store.discussion_save(scope, session, documents)
    if operation == "remember":
        event = _key(payload, "event")
        ideas = payload.get("ideas")
        if not isinstance(ideas, list) or len(ideas) > 3:
            raise ValueError("at most three ideas per turn")
        for idea in ideas:
            if idea.get("kind", "idea") not in {
                "idea",
                "decision",
                "question",
                "summary",
            }:
                raise ValueError("invalid context category")
            idea.setdefault("kind", "idea")
            if (
                not isinstance(idea.get("text"), str)
                or not 1 <= len(idea["text"]) <= 2000
            ):
                raise ValueError("invalid idea text")
            if (
                not isinstance(idea.get("quote"), str)
                or not 12 <= len(idea["quote"]) <= 1000
            ):
                raise ValueError("invalid idea evidence")
            if idea.get("url"):
                _address(idea["url"])
            if len(idea.get("label", "")) > 200:
                raise ValueError("invalid idea label")
            # State is fixed by the application, never granted by a model.
            idea["status"] = "extracted_unverified"
        return store.discussion_remember(scope, session, event, ideas)
    raise ValueError("unsupported context operation")
