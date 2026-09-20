import pytest

from infra.sqlite.storage import SQLiteStorage
from models.Meme import Meme


def test_the_sqlite_storage_refuses_get_by_id_instead_of_returning_none(tmp_path):
    storage = SQLiteStorage(str(tmp_path / "m.db"), Meme)
    with pytest.raises(NotImplementedError):
        storage.get_by_id("u1")
