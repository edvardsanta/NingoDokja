import importlib.util
from pathlib import Path
from types import SimpleNamespace

import pytest

from memes.safety import NsfwScreen, ScreenUnavailable

SERVICE_PATH = (
    Path(__file__).resolve().parents[2] / "dokja_services" / "dokja_meme" / "service.py"
)


def _load_meme_service():
    spec = importlib.util.spec_from_file_location(
        "meme_service_under_test", SERVICE_PATH
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _png_bytes() -> bytes:
    import cv2
    import numpy as np

    ok, buf = cv2.imencode(".png", np.zeros((8, 8, 3), np.uint8))
    assert ok
    return buf.tobytes()


class FakeResponse:
    def __init__(self, body: bytes, headers=None):
        self._body = body
        self.headers = headers or {}

    def __enter__(self):
        return self

    def __exit__(self, *_):
        return False

    def raise_for_status(self):
        pass

    def iter_content(self, chunk_size=65536):
        for i in range(0, len(self._body), chunk_size):
            yield self._body[i : i + chunk_size]


class FakeSession:
    def __init__(self, body=b"", error=None, headers=None):
        self.body, self.error, self.headers = body, error, headers
        self.calls = 0

    def get(self, url, **kwargs):
        self.calls += 1
        if self.error:
            raise self.error
        return FakeResponse(self.body, self.headers)


class FakeDetector:
    def __init__(self, detections=None, error=None):
        self.detections, self.error = detections or [], error

    def detect(self, image):
        if self.error:
            raise self.error
        return self.detections


def meme(url="https://x/a.png", title="funny", tags=""):
    return SimpleNamespace(
        url=url,
        title=title,
        tags=tags,
        source="test",
        sent_count=0,
        date_created=None,
        date_sent=None,
    )


def screen(detections=None, body=None, **kwargs):
    return NsfwScreen(
        session=kwargs.pop(
            "session", FakeSession(body if body is not None else _png_bytes())
        ),
        detector=kwargs.pop("detector", FakeDetector(detections)),
        **kwargs,
    )


def test_clean_image_is_safe():
    assert screen([{"class": "FACE_MALE", "score": 0.9}]).check(meme()).safe


def test_exposed_label_above_threshold_is_blocked():
    verdict = screen([{"class": "BUTTOCKS_EXPOSED", "score": 0.55}]).check(meme())
    assert not verdict.safe
    assert "BUTTOCKS_EXPOSED" in verdict.reason


def test_low_score_exposed_label_is_allowed():
    assert screen([{"class": "FEMALE_BREAST_EXPOSED", "score": 0.2}]).check(meme()).safe


def test_strict_mode_blocks_covered_labels_above_strict_threshold():
    verdict = screen([{"class": "BUTTOCKS_COVERED", "score": 0.57}]).check(meme())
    assert not verdict.safe
    assert "strict" in verdict.reason


def test_strict_mode_allows_low_score_covered_and_non_sexual_labels():
    detections = [
        {"class": "FEMALE_BREAST_COVERED", "score": 0.45},
        {"class": "FEET_EXPOSED", "score": 0.99},
        {"class": "BELLY_EXPOSED", "score": 0.99},
        {"class": "ARMPITS_EXPOSED", "score": 0.99},
    ]
    assert screen(detections).check(meme()).safe


def test_covered_labels_pass_when_strict_mode_is_off():
    assert (
        screen([{"class": "BUTTOCKS_COVERED", "score": 0.9}], strict=False)
        .check(meme())
        .safe
    )


def test_blocked_word_short_circuits_before_download():
    session = FakeSession(_png_bytes())
    verdict = screen(session=session).check(
        meme(title="Best NSFW pics", url="https://x/b.png")
    )
    assert not verdict.safe
    assert session.calls == 0


@pytest.mark.parametrize(
    "url",
    [
        "https://x/clip.mp4",
        "https://x/clip.MP4?token=1",
        "https://x/a/b.webm#t=3",
        "https://x/movie.mov",
    ],
)
def test_videos_are_never_approved_and_are_not_downloaded(url):
    session = FakeSession(_png_bytes())

    verdict = screen(session=session).check(meme(url=url))

    assert not verdict.safe
    assert "video" in verdict.reason
    assert session.calls == 0


def test_an_image_whose_path_mentions_a_video_format_is_still_screened():
    session = FakeSession(_png_bytes())

    verdict = screen(session=session).check(meme(url="https://x/mp4/a.png"))

    assert verdict.safe
    assert session.calls == 1


def test_blocked_word_needs_whole_word_match():
    assert screen().check(meme(title="Xxxtentacion, nudesc")).safe is True


def test_download_failure_and_undecodable_media_are_rejected():
    assert not screen(session=FakeSession(error=OSError("boom"))).check(meme()).safe
    assert not screen(body=b"not an image").check(meme()).safe


def test_oversized_download_is_rejected():
    verdict = screen(
        session=FakeSession(_png_bytes(), headers={"Content-Length": "999999999"})
    ).check(meme())
    assert not verdict.safe
    assert "too large" in verdict.reason


def test_detector_failure_raises_instead_of_approving():
    with pytest.raises(ScreenUnavailable):
        screen(detector=FakeDetector(error=RuntimeError("onnx exploded"))).check(meme())


def test_missing_model_file_raises():
    unloaded = NsfwScreen(
        model_path="/nonexistent/320n.onnx", session=FakeSession(_png_bytes())
    )
    with pytest.raises(ScreenUnavailable):
        unloaded.check(meme())


class FakeStorage:
    def __init__(self, memes):
        self.memes, self.updates = memes, []

    def get_filtered(self, **kwargs):
        return list(self.memes)

    def update_many(self, urls, updates):
        self.updates.append((list(urls), updates))


class StubScreen:
    def __init__(self, unsafe=()):
        self.unsafe, self.checked = set(unsafe), []

    def check(self, m):
        self.checked.append(m.url)
        return SimpleNamespace(safe=m.url not in self.unsafe, reason="stub")


def _service(memes, unsafe=(), screen_obj="stub"):
    module = _load_meme_service()
    storage = FakeStorage(memes)
    stub = StubScreen(unsafe) if screen_obj == "stub" else screen_obj
    return module, storage, module.MemeService(storage, [], None, screen=stub), stub


def test_service_labels_every_meme_without_dropping_unsafe_ones():
    memes = [meme(url=f"u{i}") for i in range(3)]
    _, storage, service, stub = _service(memes, unsafe={"u1"})

    result = service.fetch_unsent(limit=3)

    assert [m["url"] for m in result["memes"]] == ["u0", "u1", "u2"]
    assert [m["nsfw"]["safe"] for m in result["memes"]] == [True, False, True]
    assert result["memes"][1]["nsfw"]["reason"] == "stub"
    # Unsafe memes are still consumed, so the next fetch moves on to new ones.
    assert storage.updates[0][0] == ["u0", "u1", "u2"]
    assert storage.updates[0][1]["sent_count"] == 1
    assert len(storage.updates) == 1


def test_service_marks_screen_outage_as_unsafe_without_blocking_the_fetch():
    class Broken:
        def check(self, m):
            raise ScreenUnavailable("model gone")

    _, storage, service, _ = _service([meme(url="a")], screen_obj=Broken())

    result = service.fetch_unsent(limit=1)

    assert result["count"] == 1
    assert result["memes"][0]["nsfw"]["safe"] is False
    assert "screen unavailable" in result["memes"][0]["nsfw"]["reason"]
    assert storage.updates[0][0] == ["a"]


def test_service_without_screen_reports_everything_as_safe_and_accepts_float_limit():
    _, storage, service, _ = _service([meme(url="a"), meme(url="b")], screen_obj=None)
    result = service.fetch_unsent(limit=1.0)
    assert result["memes"][0]["nsfw"]["safe"] is True


def test_inspect_returns_detections_alongside_the_verdict():
    detections = [
        {"class": "BUTTOCKS_COVERED", "score": 0.57},
        {"class": "FACE_MALE", "score": 0.9},
    ]
    result = screen(detections).inspect(meme())
    assert not result.verdict.safe
    assert [d["class"] for d in result.detections] == ["BUTTOCKS_COVERED", "FACE_MALE"]


def test_service_screen_url_reports_detections_and_thresholds():
    stub = screen([{"class": "FEET_EXPOSED", "score": 0.8}])
    _, _, service, _ = _service([], screen_obj=stub)

    result = service.screen_url("https://example.com/a.png")

    assert result["safe"] is True
    assert result["detections"] == [{"class": "FEET_EXPOSED", "score": 0.8}]
    assert result["thresholds"] == {"exposed": 0.4, "strict": 0.5}


def test_service_screen_url_rejects_non_http_and_disabled_filter():
    _, _, service, _ = _service([])
    with pytest.raises(ValueError):
        service.screen_url("file:///etc/passwd")
    with pytest.raises(ValueError):
        service.screen_url("")

    _, _, disabled, _ = _service([], screen_obj=None)
    with pytest.raises(ValueError):
        disabled.screen_url("https://example.com/a.png")


def test_service_dispatch_routes_meme_screen_and_status_counts():
    class CountingStorage(FakeStorage):
        def get_filtered(self, **kwargs):
            return {0: ["a", "b"], 1: ["c"]}[kwargs["sent_count"]]

    module = _load_meme_service()
    service = module.MemeService(CountingStorage([]), [], None, screen=None)

    status = service.dispatch({"type": "meme.status"})

    assert (status["unsent_count"], status["sent_count"]) == (2, 1)
    assert "rejected_count" not in status


def test_torava_is_blacklisted_case_and_accent_insensitive():
    assert not screen().check(meme(title="Personagens que você TORAVA")).safe
    assert not screen().check(meme(title="oi", tags="torava,anime")).safe


def test_blacklist_matches_accented_entries_without_accents_and_vice_versa():
    assert not screen().check(meme(title="filme PORNO")).safe
    assert not screen().check(meme(title="filme Pornô")).safe
    assert not screen(blocked_words=("açaí",)).check(meme(title="Acai do bom")).safe


def test_extra_words_extend_the_blacklist():
    custom = screen(blocked_words=("nsfw", "palavra-nova"))
    assert not custom.check(meme(title="olha a palavra-nova aqui")).safe
    assert custom.check(meme(title="torava")).safe


def test_env_extra_words_are_added_to_the_builtin_list(monkeypatch):
    from memes.safety import build_screen_from_env

    monkeypatch.setenv("MEME_NSFW_EXTRA_WORDS", " zzz , Outra Palavra ,")
    built = build_screen_from_env()
    assert not built.check(
        meme(title="uma outra palavra qualquer", url="https://x/y.png")
    ).safe
    assert not built.check(meme(title="torava")).safe


@pytest.mark.parametrize(
    "title",
    [
        "Que TESÃO",
        "vídeo de Transando",
        "Ela é safada",
        "olha essa BUNDA",
        "A raba do Grok",
        "meus peitos",
        "18+ apenas",
        "rule34 do personagem",
        "hot sex tape",
        "naked truth",
        "Sexo é vida",
    ],
)
def test_expanded_blacklist_blocks(title):
    assert not screen().check(meme(title=title)).safe


@pytest.mark.parametrize(
    "title",
    [
        "Sexta-feira chegou",
        "Análise do jogo",
        "Essex vs Kent",
        "Menu do dia",
        "pelada de domingo",
        "comida gostosa",
        "Bundesliga hoje",
        "cocktail no bar",
        "Sexton, o jogador",
        "Analista de dados",
        "Rola a tela pra ver",
        "Que foda esse gol",
    ],
)
def test_expanded_blacklist_keeps_innocent_words(title):
    assert screen().check(meme(title=title)).safe


class FakeReader:
    def __init__(self, text="", error=None):
        self.text, self.error, self.calls = text, error, 0

    def read(self, image):
        self.calls += 1
        if self.error:
            raise self.error
        return self.text


def ocr_screen(text="", **kwargs):
    return screen(text_reader=FakeReader(text, kwargs.pop("error", None)), **kwargs)


def test_blacklisted_word_inside_the_image_blocks_the_meme():
    result = ocr_screen("coloque9personagens\nque voce torava e\n...os outros").inspect(
        meme(title="É ela")
    )
    assert not result.verdict.safe
    assert result.verdict.reason == "blocked word 'torava' in image text"
    assert "torava" in result.text


def test_glued_ocr_text_is_still_caught_for_long_words():
    assert not ocr_screen("quevoceTORAVAe").check(meme()).safe
    assert not ocr_screen("voce\ntorava").check(meme()).safe


def test_glued_matching_does_not_flag_innocent_text():
    text = "Sextaamenina aqui no escritorio contou uma\nhistoria de que opai dela largou elapq"
    assert ocr_screen(text).check(meme()).safe
    assert ocr_screen("ele e bissexual e ela heterossexual").check(meme()).safe
    assert ocr_screen("minha conquista").check(meme()).safe


def test_short_words_need_a_whole_word_match_in_image_text():
    assert not ocr_screen("olha o sex tape").check(meme()).safe
    assert ocr_screen("Sextou, Sexton e Essex").check(meme()).safe


def test_ocr_text_is_reported_even_for_safe_images():
    result = ocr_screen("minha conquista").inspect(meme())
    assert result.verdict.safe
    assert result.text == "minha conquista"


def test_ocr_failure_raises_instead_of_approving():
    with pytest.raises(ScreenUnavailable):
        ocr_screen(error=RuntimeError("onnx exploded")).check(meme())


def test_ocr_runs_after_the_cheap_checks_and_is_skipped_without_a_reader():
    reader = FakeReader("ok")
    NsfwScreen(
        session=FakeSession(_png_bytes()), detector=FakeDetector(), text_reader=reader
    ).check(meme(title="nsfw"))
    assert reader.calls == 0
    assert screen().check(meme()).safe


def test_text_reader_reports_missing_model_files(tmp_path):
    from memes.ocr import TextReader

    with pytest.raises(FileNotFoundError):
        TextReader(model_dir=str(tmp_path)).read(object())


def test_text_reader_joins_detected_lines():
    from memes.ocr import TextReader

    engine = lambda image: (
        [[[0, 0], "linha um", 0.9], [[0, 1], "linha dois", 0.9]],
        None,
    )
    assert TextReader(engine=engine).read(object()) == "linha um\nlinha dois"
    assert TextReader(engine=lambda image: (None, None)).read(object()) == ""


class PoolStorage(FakeStorage):
    """In-memory pool honouring the sent_count filter, for the browse/mark tests."""

    def __init__(self, memes):
        super().__init__(memes)
        self.by_url = {m.url: m for m in memes}

    def get_filtered(self, **kwargs):
        return [
            m
            for m in self.memes
            if m.sent_count == kwargs.get("sent_count", m.sent_count)
        ]

    def exists(self, url):
        return url in self.by_url


def _pool_service(memes):
    module = _load_meme_service()
    storage = PoolStorage(memes)
    return storage, module.MemeService(storage, [], None, screen=None)


def _row(url, sent_count=0):
    row = meme(url=url)
    row.sent_count = sent_count
    return row


def test_list_memes_pages_without_consuming_the_pool():
    storage, service = _pool_service(
        [_row(f"u{i}") for i in range(5)] + [_row("old", sent_count=1)]
    )

    page = service.list_memes(limit=2, offset=1)

    assert [m["url"] for m in page["memes"]] == ["u1", "u2"]
    assert (page["total"], page["offset"], page["count"], page["scope"]) == (
        5,
        1,
        2,
        "unsent",
    )
    assert storage.updates == []


def test_list_memes_scope_limits_and_validation():
    _, service = _pool_service([_row("a"), _row("b", sent_count=1)])

    assert [m["url"] for m in service.list_memes(scope="sent")["memes"]] == ["b"]
    assert service.list_memes(limit=99999)["count"] == 1
    assert service.list_memes(limit=0)["count"] == 1
    with pytest.raises(ValueError):
        service.list_memes(scope="everything")


def test_mark_sent_updates_only_known_memes():
    storage, service = _pool_service([_row("known")])

    assert service.mark_sent("known")["marked"] is True
    assert storage.updates[0][0] == ["known"]
    assert storage.updates[0][1]["sent_count"] == 1

    assert service.mark_sent("unknown") == {
        "url": "unknown",
        "marked": False,
        "reason": "not in pool",
    }
    assert len(storage.updates) == 1
    with pytest.raises(ValueError):
        service.mark_sent("  ")


def test_dispatch_routes_list_and_mark_sent_events():
    _, service = _pool_service([_row("a")])
    assert (
        service.dispatch({"type": "meme.list", "payload": {"limit": 5}})["count"] == 1
    )
    assert (
        service.dispatch({"type": "meme.mark_sent", "payload": {"url": "a"}})["marked"]
        is True
    )


def test_screen_url_applies_the_caption_to_the_blacklist():
    stub = screen([])
    _, _, service, _ = _service([], screen_obj=stub)

    assert service.screen_url("https://example.com/a.png")["safe"] is True
    blocked = service.screen_url("https://example.com/a.png", caption="que tesão")
    assert blocked["safe"] is False
    assert "blocked word" in blocked["reason"]
