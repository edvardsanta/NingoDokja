from __future__ import annotations

import os
import sys
from pathlib import Path


def _configure_legacy_imports() -> Path:
    repo_root = Path(__file__).resolve().parents[2]
    dokja_lab_root = repo_root / "dokja_lab"

    for path in (repo_root, dokja_lab_root):
        path_str = str(path)
        if path_str not in sys.path:
            sys.path.insert(0, path_str)

    return repo_root


REPO_ROOT = _configure_legacy_imports()

from attachment_fetcher import AttachmentFetcher, build_attachment_fetcher_from_env
from service import MemeService

from config import SCRAPERS as LEGACY_SCRAPERS
from infra.sqlite.storage import SQLiteStorage
from memes.embedder import OllamaEmbedder
from memes.hashtags import DEFAULT_MIN_SCORE, HashtagClassifier
from memes.safety import (
    DEFAULT_MAX_BYTES,
    DEFAULT_TIMEOUT_SECONDS,
    build_screen_from_env,
)
from models.Meme import Meme
from workers.meme_worker import MemeWorker


def resolve_db_file() -> str:
    return os.getenv("MEME_SERVICE_DB_FILE", str(REPO_ROOT / "ningo_memory.db"))


def build_scrapers() -> list:
    available = {
        scraper.source_name.strip().lower(): scraper.__class__
        for scraper in LEGACY_SCRAPERS
        if getattr(scraper, "source_name", "").strip()
    }

    requested_env = os.getenv("MEME_SERVICE_SCRAPERS", "")
    requested = [
        item.strip().lower() for item in requested_env.split(",") if item.strip()
    ]

    if not requested:
        return [scraper_cls() for scraper_cls in available.values()]

    scrapers = [available[name]() for name in requested if name in available]
    if scrapers:
        return scrapers

    return [scraper_cls() for scraper_cls in available.values()]


def resolve_hashtag_db_file() -> str:
    return os.getenv("MEME_HASHTAG_DB_FILE", str(REPO_ROOT / "ningo_hashtags.db"))


def build_hashtag_classifier() -> HashtagClassifier:
    model = os.getenv("DOKJA_EMBED_MODEL", "bge-m3")
    embedder = None
    if os.getenv("DOKJA_EMBED", "on").strip().lower() not in {
        "off",
        "0",
        "false",
        "none",
    }:
        embedder = OllamaEmbedder(
            os.getenv("DOKJA_EMBED_ENDPOINT", "http://127.0.0.1:11434"),
            model,
            timeout=float(os.getenv("DOKJA_EMBED_TIMEOUT", "30")),
        )
    return HashtagClassifier(
        resolve_hashtag_db_file(),
        embedder=embedder,
        model=model,
        min_score=float(os.getenv("MEME_HASHTAG_MIN_SCORE", str(DEFAULT_MIN_SCORE))),
    )


def build_service() -> MemeService:
    storage = SQLiteStorage(resolve_db_file(), Meme)
    scrapers = build_scrapers()
    worker = MemeWorker(scrapers=scrapers, storage=storage)
    # OCR needs the same browser-compatible transport as delivery, with its own
    # existing limits. Disabling attachment delivery must not disable OCR.
    ocr_fetcher = AttachmentFetcher(
        max_bytes=DEFAULT_MAX_BYTES, timeout=DEFAULT_TIMEOUT_SECONDS
    )
    return MemeService(
        storage=storage,
        scrapers=scrapers,
        worker=worker,
        screen=build_screen_from_env(downloader=lambda url: ocr_fetcher.fetch(url)[0]),
        hashtags=build_hashtag_classifier(),
        attachments=build_attachment_fetcher_from_env(),
    )
