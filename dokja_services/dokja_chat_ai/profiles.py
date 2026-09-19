"""Where the chat service gets its provider credentials.

The selected profile in the shared SQLite file wins; without one, the CHAT_AI_* environment
variables still work. The file is re-read on every request, so switching profiles needs no
restart, and the OpenAI client is rebuilt only when the selection actually changed.
"""

from __future__ import annotations

import logging
import os
import sqlite3
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping

logger = logging.getLogger("dokja_chat_ai.profiles")

_ACTIVE_PROFILE_QUERY = """
    SELECT p.name, p.base_url, p.model, p.api_key, p.updated_at
    FROM settings s JOIN chat_profiles p ON p.name = s.value
    WHERE s.key = 'chat_active_profile'
"""


class NotConfigured(RuntimeError):
    """No profile is selected and the environment has no key either."""


@dataclass(frozen=True)
class Credentials:
    source: str  # "profile" or "env"
    name: str
    base_url: str
    model: str
    version: str  # changes when the profile is edited, so a new key is picked up
    api_key: str = field(repr=False)  # never printed, even by accident

    def fingerprint(self) -> tuple:
        return (self.source, self.name, self.base_url, self.model, self.version)

    def describe(self) -> dict:
        """What health checks may show: everything but the key."""
        return {
            "source": self.source,
            "profile": self.name,
            "base_url": self.base_url,
            "model": self.model,
            "has_key": bool(self.api_key),
        }


class CredentialSource:
    def __init__(self, db_path: str | None = None, env: Mapping[str, str] | None = None):
        self.db_path = db_path or None
        self.env = os.environ if env is None else env

    def current(self) -> Credentials | None:
        credentials = self._from_profile()
        if credentials is not None:
            return credentials
        return self._from_env()

    def _from_profile(self) -> Credentials | None:
        if not self.db_path or not Path(self.db_path).exists():
            return None
        try:
            # query_only: this service reads the file and must never change it.
            connection = sqlite3.connect(self.db_path, timeout=3)
            try:
                connection.execute("PRAGMA query_only = ON")
                row = connection.execute(_ACTIVE_PROFILE_QUERY).fetchone()
            finally:
                connection.close()
        except sqlite3.Error as error:
            logger.warning("could not read chat profiles from %s: %s", self.db_path, error)
            return None
        if row is None:
            return None
        name, base_url, model, api_key, updated_at = row
        return Credentials("profile", name, base_url, model, updated_at, api_key)

    def _from_env(self) -> Credentials | None:
        api_key = self.env.get("CHAT_AI_API_KEY", "").strip()
        if not api_key:
            return None
        return Credentials(
            "env",
            "env",
            self.env.get("CHAT_AI_BASE_URL", ""),
            self.env.get("CHAT_AI_MODEL", "n/a"),
            "",
            api_key,
        )


class ChatServiceProvider:
    """Hands out a chat service built from the current credentials."""

    def __init__(self, source: CredentialSource, factory: Callable[..., object] | None = None):
        self.source = source
        self._factory = factory
        self._service = None
        self._fingerprint = None

    def _build(self, credentials: Credentials):
        factory = self._factory
        if factory is None:
            from openai_chat_service import OpenAIChatService

            factory = OpenAIChatService
        return factory(api_key=credentials.api_key, base_url=credentials.base_url, model=credentials.model)

    def get(self):
        credentials = self.source.current()
        if credentials is None:
            raise NotConfigured("no chat profile is selected and CHAT_AI_API_KEY is not set")
        fingerprint = credentials.fingerprint()
        if fingerprint != self._fingerprint:
            self._service = self._build(credentials)
            self._fingerprint = fingerprint
            logger.info(
                "chat credentials in use source=%s profile=%s model=%s base_url=%s",
                credentials.source,
                credentials.name,
                credentials.model,
                credentials.base_url,
            )
        return self._service

    def health(self) -> dict:
        """No provider call here: the panel polls this every few seconds."""
        credentials = self.source.current()
        if credentials is None:
            raise NotConfigured("no chat profile is selected and CHAT_AI_API_KEY is not set")
        return {"status": "ok", **credentials.describe()}
