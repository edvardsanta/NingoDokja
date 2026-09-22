import zlib

import numpy as np
import pytest

from memes.embedder import EmbedUnavailable, normalize
from memes.hashtags import DEFAULT_MIN_SCORE, HashtagClassifier, normalize_hashtag


class FakeEmbedder:
    """Bag-of-words hashing embedder: texts sharing words end up close, texts sharing
    none end up near-orthogonal. Deterministic and offline, like the one the knowledge
    base tests use, so this tests the classifier's logic and not a real model.
    """

    def __init__(self, dimensions: int = 256):
        self.dimensions = dimensions
        self.down = False
        self.calls: list[list[str]] = []

    def embed(self, texts):
        self.calls.append(list(texts))
        if self.down:
            raise EmbedUnavailable("embedding server unreachable: connection refused")
        matrix = np.zeros((len(texts), self.dimensions), dtype=np.float32)
        for row, text in enumerate(texts):
            for word in text.lower().split():
                matrix[row, zlib.crc32(word.encode()) % self.dimensions] += 1.0
        return normalize(matrix)


@pytest.fixture
def embedder():
    return FakeEmbedder()


@pytest.fixture
def classifier(embedder):
    return HashtagClassifier(":memory:", embedder=embedder, model="fake")


# ---- normalize_hashtag ----------------------------------------------------------------


@pytest.mark.parametrize(
    "raw, expected",
    [
        ("GuiConversinhas", "#GuiConversinhas"),
        ("#GuiConversinhas", "#GuiConversinhas"),
        ("  tio_do_pave  ", "#tio_do_pave"),
    ],
)
def test_normalize_hashtag_adds_the_hash_and_trims(raw, expected):
    assert normalize_hashtag(raw) == expected


@pytest.mark.parametrize("raw", ["", "   ", "#", "#tio pave", "#tio-pave", "#tio#pave"])
def test_normalize_hashtag_rejects_whats_not_a_single_tag(raw):
    with pytest.raises(ValueError):
        normalize_hashtag(raw)


# ---- tagging ----------------------------------------------------------------------------


def test_tagging_stores_the_example_with_its_embedding(classifier):
    result = classifier.tag(
        "https://x/a.jpg", "TioDoPave", "o tio pegou o pave e comeu escondido"
    )

    assert result == {
        "source_url": "https://x/a.jpg",
        "hashtag": "#TioDoPave",
        "text": "o tio pegou o pave e comeu escondido",
        "embedded": True,
    }
    listed = classifier.list_examples()
    assert listed["total"] == 1 and listed["examples"][0]["embedded"] is True


def test_tagging_the_same_url_again_replaces_the_example(classifier):
    classifier.tag("https://x/a.jpg", "TioDoPave", "primeira versao do texto")
    classifier.tag("https://x/a.jpg", "TioDoPave", "segunda versao do texto")

    listed = classifier.list_examples()
    assert listed["total"] == 1
    assert listed["examples"][0]["text"] == "segunda versao do texto"


def test_tagging_without_an_embedder_still_stores_the_text(embedder):
    classifier = HashtagClassifier(":memory:", embedder=None, model="fake")
    result = classifier.tag("https://x/a.jpg", "TioDoPave", "o tio e o pave")
    assert result["embedded"] is False and result["degraded"] is True
    assert classifier.list_examples()["examples"][0]["embedded"] is False


def test_tagging_when_the_embedder_is_down_stores_the_text_and_says_so(
    classifier, embedder
):
    embedder.down = True
    result = classifier.tag("https://x/a.jpg", "TioDoPave", "o tio e o pave")
    assert result["embedded"] is False and "unreachable" in result["reason"]


@pytest.mark.parametrize(
    "source_url, hashtag, text, message",
    [
        ("", "TioDoPave", "algum texto", "source_url"),
        ("https://x/a.jpg", "not a tag!", "algum texto", "invalid hashtag"),
        ("https://x/a.jpg", "TioDoPave", "   ", "text is required"),
        ("https://x/a.jpg", "TioDoPave", "", "text is required"),
    ],
)
def test_tag_validates_its_input_and_stores_nothing(
    classifier, source_url, hashtag, text, message
):
    with pytest.raises(ValueError, match=message):
        classifier.tag(source_url, hashtag, text)
    assert classifier.list_examples()["total"] == 0


def test_delete_removes_an_example(classifier):
    classifier.tag("https://x/a.jpg", "TioDoPave", "texto")
    assert classifier.delete("https://x/a.jpg") is True
    assert classifier.delete("https://x/a.jpg") is False
    assert classifier.list_examples()["total"] == 0


def test_list_examples_pages_newest_first(classifier):
    for i in range(3):
        classifier.tag(f"https://x/{i}.jpg", "Tag", f"texto numero {i}")
    page = classifier.list_examples(limit=2)
    assert page["total"] == 3 and len(page["examples"]) == 2
    assert page["examples"][0]["source_url"] == "https://x/2.jpg"


# ---- suggestion (this is the point of the feature) -------------------------------------


def test_a_new_meme_gets_the_hashtag_of_the_closest_tagged_example(classifier):
    classifier.tag(
        "https://x/1.jpg", "TioDoPave", "o tio pegou o pave da geladeira escondido"
    )
    classifier.tag(
        "https://x/2.jpg", "GatoBrabo", "o gato virou a mesa de vidro e fugiu"
    )

    result = classifier.suggest("o tio comeu o pave escondido de novo")

    assert result["hashtag"] == "#TioDoPave"
    assert result["relevant"] is True
    assert result["matched_source_url"] == "https://x/1.jpg"
    assert result["examples"] == 2
    assert result["degraded"] is False


def test_text_unlike_any_example_gets_no_suggestion(classifier):
    classifier.tag(
        "https://x/1.jpg", "TioDoPave", "o tio pegou o pave da geladeira escondido"
    )

    result = classifier.suggest("relatorio trimestral de vendas da equipe comercial")

    assert result["hashtag"] == "#TioDoPave", "the nearest neighbour is still reported"
    assert result["relevant"] is False, "but it must not be presented as a match"
    assert result["score"] < result["threshold"]


def test_scores_are_not_constant_across_queries(classifier):
    classifier.tag(
        "https://x/1.jpg", "TioDoPave", "o tio pegou o pave da geladeira escondido"
    )
    classifier.tag(
        "https://x/2.jpg", "GatoBrabo", "o gato virou a mesa de vidro e fugiu"
    )
    close = classifier.suggest("o tio comeu o pave escondido de novo")["score"]
    far = classifier.suggest("relatorio de vendas da equipe")["score"]
    assert close > far


def test_suggest_with_no_examples_yet_is_reported_without_being_an_error(classifier):
    result = classifier.suggest("qualquer coisa")
    assert result == {
        "query_text": "qualquer coisa",
        "hashtag": None,
        "score": None,
        "relevant": False,
        "threshold": DEFAULT_MIN_SCORE,
        "degraded": False,
        "reason": "no tagged examples with an embedding from this model yet",
        "examples": 0,
    }


def test_suggest_without_an_embedder_is_degraded(embedder):
    classifier = HashtagClassifier(":memory:", embedder=None, model="fake")
    result = classifier.suggest("qualquer coisa")
    assert result["degraded"] is True and result["hashtag"] is None
    assert "no embedding server" in result["reason"]


def test_suggest_when_the_embedder_is_down_is_degraded(classifier, embedder):
    classifier.tag("https://x/1.jpg", "TioDoPave", "o tio pegou o pave")
    embedder.down = True
    result = classifier.suggest("o tio pegou o pave de novo")
    assert result["degraded"] is True and "unreachable" in result["reason"]


def test_examples_from_another_embedding_model_are_not_used(embedder):
    conn = HashtagClassifier(":memory:", embedder=embedder, model="old")
    conn.tag("https://x/1.jpg", "TioDoPave", "o tio pegou o pave")
    conn.model = "new"  # simulate DOKJA_EMBED_MODEL changing between runs
    result = conn.suggest("o tio pegou o pave de novo")
    assert result["hashtag"] is None
    assert "no tagged examples" in result["reason"]


def test_the_threshold_can_be_loosened_or_tightened_per_call(classifier):
    classifier.tag(
        "https://x/1.jpg", "TioDoPave", "o tio pegou o pave da geladeira escondido"
    )
    loose = classifier.suggest("relatorio de vendas", min_score=0.0)
    assert loose["relevant"] is True
    strict = classifier.suggest("o tio pegou o pave", min_score=0.999)
    assert strict["threshold"] == 0.999


def test_suggest_validates_its_input(classifier):
    with pytest.raises(ValueError):
        classifier.suggest("   ")


def test_examples_persist_to_the_same_file_across_connections(tmp_path, embedder):
    db_file = str(tmp_path / "hashtags.db")
    first = HashtagClassifier(db_file, embedder=embedder, model="fake")
    first.tag("https://x/1.jpg", "TioDoPave", "o tio pegou o pave")
    first.close()

    second = HashtagClassifier(db_file, embedder=embedder, model="fake")
    assert second.list_examples()["total"] == 1
    assert second.suggest("o tio pegou o pave de novo")["hashtag"] == "#TioDoPave"
