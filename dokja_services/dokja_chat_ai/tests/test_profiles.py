import logging
import sqlite3

import pytest

from profiles import ChatServiceProvider, CredentialSource, Credentials, NotConfigured

SECRET = "sk-or-v1-SUPERSECRETTOKEN-abcd1234"

# The same tables dokja_store creates (kept in step by a Go test on the column names).
SCHEMA = """
CREATE TABLE chat_profiles (
    name TEXT PRIMARY KEY, base_url TEXT NOT NULL, model TEXT NOT NULL, api_key TEXT NOT NULL,
    key_hint TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);
"""


@pytest.fixture
def db(tmp_path):
    path = tmp_path / "dokja.db"
    connection = sqlite3.connect(path)
    connection.executescript(SCHEMA)
    connection.commit()
    connection.close()
    return path


def add_profile(path, name, base_url="https://one.example/v1", model="m1", key=SECRET, version="v1"):
    connection = sqlite3.connect(path)
    connection.execute(
        "INSERT OR REPLACE INTO chat_profiles VALUES (?, ?, ?, ?, ?, ?, ?)",
        (name, base_url, model, key, "…1234", "t", version),
    )
    connection.commit()
    connection.close()


def select(path, name):
    connection = sqlite3.connect(path)
    if name is None:
        connection.execute("DELETE FROM settings WHERE key = 'chat_active_profile'")
    else:
        connection.execute(
            "INSERT OR REPLACE INTO settings VALUES ('chat_active_profile', ?, 't')", (name,)
        )
    connection.commit()
    connection.close()


class RecordingFactory:
    def __init__(self):
        self.calls = []

    def __call__(self, **kwargs):
        self.calls.append(kwargs)
        return object()


def provider_for(path, env=None, factory=None):
    factory = factory or RecordingFactory()
    return ChatServiceProvider(CredentialSource(str(path) if path else None, env or {}), factory), factory


def test_nothing_configured_is_reported_not_crashed(tmp_path):
    provider, factory = provider_for(tmp_path / "missing.db")
    with pytest.raises(NotConfigured):
        provider.get()
    with pytest.raises(NotConfigured):
        provider.health()
    assert factory.calls == []


def test_environment_settings_still_work_without_a_database():
    provider, factory = provider_for(None, {"CHAT_AI_API_KEY": SECRET, "CHAT_AI_BASE_URL": "https://env/v1", "CHAT_AI_MODEL": "envmodel"})
    provider.get()
    assert factory.calls == [{"api_key": SECRET, "base_url": "https://env/v1", "model": "envmodel"}]
    assert provider.health()["source"] == "env"


def test_the_selected_profile_wins_over_the_environment(db):
    add_profile(db, "hosted", "https://api.example.com/v1", "llama")
    select(db, "hosted")
    provider, factory = provider_for(db, {"CHAT_AI_API_KEY": "sk-env-key", "CHAT_AI_BASE_URL": "https://env/v1"})

    provider.get()

    assert factory.calls == [{"api_key": SECRET, "base_url": "https://api.example.com/v1", "model": "llama"}]
    assert provider.health()["profile"] == "hosted"


def test_a_profile_that_exists_but_is_not_selected_is_ignored(db):
    add_profile(db, "hosted")
    provider, _ = provider_for(db)
    with pytest.raises(NotConfigured):
        provider.get()


def test_switching_profiles_rebuilds_the_client_on_the_next_request(db):
    add_profile(db, "one", "https://one.example/v1", "m1", "sk-key-one-aaaa1111")
    add_profile(db, "two", "https://two.example/v1", "m2", "sk-key-two-bbbb2222")
    select(db, "one")
    provider, factory = provider_for(db)

    provider.get()
    provider.get()
    assert len(factory.calls) == 1, "an unchanged selection must reuse the client"

    select(db, "two")
    provider.get()

    assert len(factory.calls) == 2
    assert factory.calls[1] == {"api_key": "sk-key-two-bbbb2222", "base_url": "https://two.example/v1", "model": "m2"}

    select(db, "one")
    provider.get()
    assert factory.calls[2]["base_url"] == "https://one.example/v1"


def test_editing_the_selected_profile_picks_up_the_new_key(db):
    add_profile(db, "one", key="sk-old-key-000011112222", version="v1")
    select(db, "one")
    provider, factory = provider_for(db)
    provider.get()

    add_profile(db, "one", key="sk-new-key-999988887777", version="v2")
    provider.get()

    assert [call["api_key"] for call in factory.calls] == ["sk-old-key-000011112222", "sk-new-key-999988887777"]


def test_deselecting_falls_back_to_the_environment(db):
    add_profile(db, "one")
    select(db, "one")
    provider, factory = provider_for(db, {"CHAT_AI_API_KEY": "sk-env-fallback-1234", "CHAT_AI_BASE_URL": "https://env/v1"})
    provider.get()

    select(db, None)
    provider.get()

    assert factory.calls[-1]["base_url"] == "https://env/v1"
    assert provider.health()["source"] == "env"


def test_an_unreadable_database_falls_back_instead_of_failing(tmp_path):
    broken = tmp_path / "dokja.db"
    broken.write_bytes(b"this is definitely not a sqlite database, just some long text to look like one")
    provider, factory = provider_for(broken, {"CHAT_AI_API_KEY": "sk-env-fallback-1234"})

    provider.get()

    assert factory.calls[0]["api_key"] == "sk-env-fallback-1234"


def test_health_never_builds_a_client_or_calls_the_provider(db):
    add_profile(db, "one")
    select(db, "one")

    def forbidden(**_):
        raise AssertionError("health must not touch the provider")

    provider = ChatServiceProvider(CredentialSource(str(db), {}), forbidden)
    health = provider.health()

    assert health["status"] == "ok" and health["has_key"] is True


def test_the_key_never_shows_in_health_repr_or_logs(db, caplog):
    add_profile(db, "one")
    select(db, "one")
    provider, _ = provider_for(db)

    with caplog.at_level(logging.DEBUG):
        provider.get()
        health = provider.health()
    credentials = provider.source.current()

    assert SECRET not in str(health)
    assert SECRET not in repr(credentials)
    assert SECRET not in caplog.text
    assert "SUPERSECRET" not in caplog.text + repr(credentials) + str(health)
    assert "api_key" not in health


def test_the_reader_cannot_modify_the_database(db):
    add_profile(db, "one")
    select(db, "one")
    source = CredentialSource(str(db), {})
    before = sqlite3.connect(db).execute("SELECT COUNT(*) FROM chat_profiles").fetchone()

    source.current()
    connection = sqlite3.connect(db)
    connection.execute("PRAGMA query_only = ON")
    with pytest.raises(sqlite3.OperationalError):
        connection.execute("DELETE FROM chat_profiles")

    assert sqlite3.connect(db).execute("SELECT COUNT(*) FROM chat_profiles").fetchone() == before


def test_credentials_object_hides_the_key_in_repr():
    credentials = Credentials("profile", "x", "https://x", "m", "v", SECRET)
    assert SECRET not in repr(credentials)
    assert credentials.describe()["has_key"] is True
