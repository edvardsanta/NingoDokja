from __future__ import annotations

import logging
import os

from embedder import DEFAULT_ENDPOINT, DEFAULT_MODEL, OllamaEmbedder
from extractors import default_registry
from plugins import load_plugins
from service import DEFAULT_MIN_SCORE, KnowledgeService
from store import Store

logger = logging.getLogger("dokja_knowledge.bootstrap")


def resolve_db_file() -> str:
    return os.getenv("KNOWLEDGE_DB_FILE", "dokja_knowledge.db")


def build_service() -> KnowledgeService:
    endpoint = os.getenv("DOKJA_EMBED_ENDPOINT", DEFAULT_ENDPOINT)
    model = os.getenv("DOKJA_EMBED_MODEL", DEFAULT_MODEL)
    embedder = None
    if os.getenv("DOKJA_EMBED", "on").strip().lower() not in {"off", "0", "false", "none"}:
        embedder = OllamaEmbedder(endpoint, model, timeout=float(os.getenv("DOKJA_EMBED_TIMEOUT", "60")))
    else:
        logger.warning("embeddings are disabled (DOKJA_EMBED=off); only keyword search will work")
    registry = default_registry()
    plugins_dir = os.getenv("KNOWLEDGE_PLUGINS_DIR", "").strip()
    if plugins_dir:
        logger.info("plugins loaded: %s", ", ".join(load_plugins(registry, plugins_dir)) or "(none)")
    fetch_enabled = os.getenv("KNOWLEDGE_FETCH", "on").strip().lower() not in {"off", "0", "false", "none"}
    if not fetch_enabled:
        logger.warning("fetching addresses is disabled (KNOWLEDGE_FETCH=off)")
    return KnowledgeService(
        Store(resolve_db_file()),
        embedder=embedder,
        model=model,
        min_score=float(os.getenv("KNOWLEDGE_MIN_SCORE", str(DEFAULT_MIN_SCORE))),
        registry=registry,
        fetch_enabled=fetch_enabled,
    )
