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

from infra.sqlite.storage import SQLiteStorage
from memes.ifunny_scraper import IfunnyScraper
from memes.memedroid_scraper import MemedroidScraper
from models.Meme import Meme
from workers.meme_worker import MemeWorker

from service import MemeService


def resolve_db_file() -> str:
    return os.getenv("MEME_SERVICE_DB_FILE", str(REPO_ROOT / "ningo_memory.db"))


def build_scrapers() -> list:
    requested = os.getenv("MEME_SERVICE_SCRAPERS", "memedroid").split(",")
    enabled = {item.strip().lower() for item in requested if item.strip()}

    scrapers = []
    if "memedroid" in enabled:
        scrapers.append(MemedroidScraper())
    if "ifunny" in enabled:
        scrapers.append(IfunnyScraper())

    if not scrapers:
        scrapers.append(MemedroidScraper())

    return scrapers


def build_service() -> MemeService:
    storage = SQLiteStorage(resolve_db_file(), Meme)
    scrapers = build_scrapers()
    worker = MemeWorker(scrapers=scrapers, storage=storage)
    return MemeService(storage=storage, scrapers=scrapers, worker=worker)
