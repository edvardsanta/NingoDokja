"""Step 03 baseline (steps.json): how the meme pool behaves today.

ADR 0007 turns this pool into a Reading cache, screening into Moderation and sent
marks into Experience receipts. These tests pin the current behavior those moves
must keep behind the meme.* facade, or change deliberately.
"""

import pytest
from infra.sqlite.storage import SQLiteStorage
from memes.safety import ScreenUnavailable, Verdict
from models.Meme import Meme
from service import MemeService

FIRST_URL = "https://cdn.example/a.png"
SECOND_URL = "https://cdn.example/b.png"


class FakeScreen:
    def __init__(self, verdicts=None, error=None):
        self.verdicts = verdicts or {}
        self.error = error

    def check(self, meme):
        if self.error is not None:
            raise self.error
        return self.verdicts.get(meme.url, Verdict(True, "clean"))


@pytest.fixture
def storage(tmp_path):
    store = SQLiteStorage(str(tmp_path / "memes.db"), Meme)
    store.add(Meme(url=FIRST_URL, title="first", source="test"))
    store.add(Meme(url=SECOND_URL, title="second", source="test"))
    return store


def service_with(storage, screen=None):
    return MemeService(storage=storage, scrapers=[], worker=None, screen=screen)


def test_fetching_keeps_memes_unsent_until_delivery_is_confirmed(storage):
    service = service_with(storage)

    fetched = service.dispatch({"type": "meme.fetch", "payload": {}})

    assert fetched["count"] == 2
    assert service.status()["unsent_count"] == 2
    assert service.status()["sent_count"] == 0
    assert service.dispatch({"type": "meme.fetch", "payload": {}})["count"] == 2
    service.mark_sent(FIRST_URL)
    assert service.dispatch({"type": "meme.fetch", "payload": {}})["count"] == 1


def test_listing_browses_without_consuming(storage):
    service = service_with(storage)

    listed = service.dispatch({"type": "meme.list", "payload": {"limit": 1}})

    assert (listed["scope"], listed["total"], listed["count"]) == ("unsent", 2, 1)
    assert service.status()["unsent_count"] == 2
    with pytest.raises(ValueError):
        service.list_memes(scope="everything")


def test_screening_labels_but_does_not_filter(storage):
    screen = FakeScreen({SECOND_URL: Verdict(False, "exposed")})
    service = service_with(storage, screen)

    memes = {m["url"]: m["nsfw"] for m in service.fetch_unsent()["memes"]}

    assert memes[FIRST_URL] == {"safe": True, "reason": "clean"}
    assert memes[SECOND_URL] == {"safe": False, "reason": "exposed"}


def test_an_unavailable_screen_fails_closed_without_blocking_the_fetch(storage):
    service = service_with(
        storage, FakeScreen(error=ScreenUnavailable("model missing"))
    )

    fetched = service.fetch_unsent()

    assert fetched["count"] == 2
    assert all(m["nsfw"]["safe"] is False for m in fetched["memes"])
    assert all("screen unavailable" in m["nsfw"]["reason"] for m in fetched["memes"])


def test_without_a_screen_every_meme_is_labelled_safe(storage):
    fetched = service_with(storage).fetch_unsent(limit=1)

    assert fetched["memes"][0]["nsfw"] == {"safe": True, "reason": "filter disabled"}


def test_marking_sent_only_touches_pooled_urls(storage):
    service = service_with(storage)

    assert service.mark_sent(FIRST_URL) == {"url": FIRST_URL, "marked": True}
    assert service.mark_sent("https://cdn.example/elsewhere.png")["marked"] is False
    assert service.status()["sent_count"] == 1
    with pytest.raises(ValueError):
        service.mark_sent(" ")


def test_unknown_events_are_refused(storage):
    with pytest.raises(ValueError, match="unsupported meme event type"):
        service_with(storage).dispatch({"type": "meme.unknown", "payload": {}})
