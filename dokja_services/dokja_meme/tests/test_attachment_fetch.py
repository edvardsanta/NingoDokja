import base64

import pytest
from attachment_fetcher import AttachmentFetchError
from infra.sqlite.storage import SQLiteStorage
from models.Meme import Meme
from service import MemeService

VIDEO_URL = "https://cdn.example/videos/clip.mp4"


class FakeAttachments:
    def __init__(self, content=b"video-bytes", content_type="video/mp4", error=None):
        self.content = content
        self.content_type = content_type
        self.error = error
        self.calls = []

    def fetch(self, url):
        self.calls.append(url)
        if self.error is not None:
            raise self.error
        return self.content, self.content_type


@pytest.fixture
def storage(tmp_path):
    store = SQLiteStorage(str(tmp_path / "memes.db"), Meme)
    store.add(Meme(url=VIDEO_URL, title="a video", source="test"))
    return store


def service_with(storage, attachments):
    return MemeService(
        storage=storage, scrapers=[], worker=None, attachments=attachments
    )


def test_fetch_attachment_returns_base64_bytes_for_a_pooled_url(storage):
    attachments = FakeAttachments(content=b"\x00\x01video", content_type="video/mp4")
    service = service_with(storage, attachments)

    result = service.dispatch(
        {"type": "meme.attachment.fetch", "payload": {"url": VIDEO_URL}}
    )

    assert attachments.calls == [VIDEO_URL]
    assert result["content_type"] == "video/mp4"
    assert result["size"] == len(b"\x00\x01video")
    assert base64.b64decode(result["data_b64"]) == b"\x00\x01video"


def test_fetch_attachment_refuses_a_url_outside_the_pool(storage):
    service = service_with(storage, FakeAttachments())

    with pytest.raises(ValueError, match="not in this service's meme pool"):
        service.fetch_attachment("https://cdn.example/videos/unknown.mp4")


def test_fetch_attachment_is_refused_when_disabled(storage):
    service = service_with(storage, attachments=None)

    with pytest.raises(ValueError, match="MEME_ATTACHMENT_FETCH=off"):
        service.fetch_attachment(VIDEO_URL)


def test_fetch_attachment_surfaces_a_download_failure(storage):
    attachments = FakeAttachments(error=AttachmentFetchError("host returned 403"))
    service = service_with(storage, attachments)

    with pytest.raises(ValueError, match="403"):
        service.fetch_attachment(VIDEO_URL)
