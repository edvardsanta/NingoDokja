from unittest.mock import Mock

from handlers.meme_handler import MemeHandler
from models.Meme import Meme


def test_without_storage_there_is_nothing_to_send():
    assert MemeHandler().handle({}) == []


def test_with_no_unsent_memes_nothing_is_marked():
    storage = Mock()
    storage.get_filtered.return_value = []

    assert MemeHandler(storage).handle({}) == []
    storage.update_many.assert_not_called()


def test_unsent_memes_are_returned_and_marked_as_sent():
    storage = Mock()
    storage.get_filtered.return_value = [
        Meme(url="u1", title="one", source="s"),
        Meme(url="u2", title="two", source="s"),
    ]

    result = MemeHandler(storage).handle({})

    assert [meme["url"] for meme in result] == ["u1", "u2"]
    storage.get_filtered.assert_called_once_with(sent_count=0)
    ids, updates = storage.update_many.call_args.args
    assert ids == ["u1", "u2"] and updates["sent_count"] == 1 and updates["date_sent"]
