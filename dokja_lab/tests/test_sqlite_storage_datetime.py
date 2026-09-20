"""Behaviour of the SQLite storage around datetime fields.

The storage decides which columns hold datetimes by reflecting on the dataclass annotations, so
these tests pin what it stores. They must pass whether a nullable datetime field is annotated
`datetime` or `Optional[datetime]`.
"""

import sqlite3
from datetime import datetime

import pytest

from infra.sqlite.storage import SQLiteStorage
from models.Meme import Meme


@pytest.fixture
def storage(tmp_path):
    return SQLiteStorage(str(tmp_path / "memes.db"), Meme)


def raw_row(storage, url):
    storage.conn.row_factory = sqlite3.Row
    return dict(
        storage.conn.execute("SELECT * FROM meme WHERE url = ?", (url,)).fetchone()
    )


def test_the_table_stores_datetimes_as_text_and_counters_as_integers(storage):
    columns = {
        row[1]: row[2]
        for row in storage.conn.execute("PRAGMA table_info(meme)").fetchall()
    }
    assert columns["date_created"] == "TEXT"
    assert columns["date_sent"] == "TEXT"
    assert columns["sent_count"] == "INTEGER"


def test_a_new_meme_has_no_sent_date_and_a_creation_date(storage):
    assert storage.add(Meme(url="u1", title="t", source="s"))
    row = raw_row(storage, "u1")
    assert row["date_sent"] is None
    assert row["date_created"] and "T" in row["date_created"], "stored as an ISO string"


def test_update_many_stores_a_datetime_as_an_iso_string(storage):
    storage.add(Meme(url="u1", title="t", source="s"))
    sent_at = datetime(2026, 1, 2, 3, 4, 5)

    storage.update_many(["u1"], {"sent_count": 1, "date_sent": sent_at})

    row = raw_row(storage, "u1")
    assert row["date_sent"] == "2026-01-02T03:04:05"
    assert row["sent_count"] == 1


def test_update_many_leaves_other_values_alone(storage):
    storage.add(Meme(url="u1", title="t", source="s"))
    storage.update_many(["u1"], {"title": "renamed", "date_sent": None})
    row = raw_row(storage, "u1")
    assert row["title"] == "renamed" and row["date_sent"] is None
