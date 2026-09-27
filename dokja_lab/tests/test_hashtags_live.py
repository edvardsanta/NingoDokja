"""Hashtag suggestion quality against a real bge-m3. Skipped when no Ollama server is
reachable.

The examples and the questions are in Portuguese on purpose, in the style of real meme
captions (short, informal, no punctuation), and check that a paraphrase with no shared
words still finds the right tagged example. DEFAULT_MIN_SCORE in memes/hashtags.py was
checked against this set; do not translate it without re-checking the scores.
"""

import pytest

from memes.embedder import OllamaEmbedder
from memes.hashtags import DEFAULT_MIN_SCORE, HashtagClassifier

EXAMPLES = {
    "https://x/pave1.png": (
        "TioDoPave",
        "eu perguntei quem comeu o pave da geladeira e ninguem respondeu",
    ),
    "https://x/pave2.png": (
        "TioDoPave",
        "aquele tio que sempre come o doce escondido na festa de familia",
    ),
    "https://x/gato1.png": (
        "GatoBrabo",
        "o gato derrubou o vaso e saiu correndo igual nada aconteceu",
    ),
    "https://x/trabalho1.png": (
        "SegundaFeira",
        "quando o chefe manda mensagem sexta a noite pedindo relatorio",
    ),
}
# Paraphrases: they share (almost) no words with the example they must find.
PARAPHRASES = {
    "TioDoPave": "de que forma sumiu a sobremesa da geladeira sem ninguem admitir",
    "GatoBrabo": "o bichano quebrou o vaso e fugiu que nem nada tivesse acontecido",
    "SegundaFeira": "mensagem do chefe chegando bem na hora que voce ia descansar",
}
UNRELATED = [
    "relatorio trimestral do departamento financeiro da empresa",
    "qual a capital da franca mesmo mano",
]


@pytest.fixture(scope="module")
def classifier():
    embedder = OllamaEmbedder(timeout=60)
    if not embedder.reachable():
        pytest.skip("Ollama is not running")
    classifier = HashtagClassifier(":memory:", embedder=embedder, model="bge-m3")
    for url, (hashtag, text) in EXAMPLES.items():
        result = classifier.tag(url, hashtag, text)
        if not result["embedded"]:
            pytest.skip(f"bge-m3 is not usable: {result.get('reason')}")
    return classifier


@pytest.mark.parametrize("hashtag", list(PARAPHRASES))
def test_a_paraphrase_finds_its_hashtag_without_sharing_words(classifier, hashtag):
    result = classifier.suggest(PARAPHRASES[hashtag])
    assert result["hashtag"] == "#" + hashtag
    assert result["relevant"], f"score {result['score']} under {result['threshold']}"


@pytest.mark.parametrize("text", UNRELATED)
def test_unrelated_captions_stay_under_the_threshold(classifier, text):
    result = classifier.suggest(text)
    assert not result["relevant"], (result["hashtag"], result["score"])


def test_print_score_distribution(classifier, capsys):
    """Not an assertion: shows the numbers behind DEFAULT_MIN_SCORE."""
    with capsys.disabled():
        print(f"\nDEFAULT_MIN_SCORE={DEFAULT_MIN_SCORE}")
        for label, texts in (
            ("related", PARAPHRASES.values()),
            ("unrelated", UNRELATED),
        ):
            for text in texts:
                result = classifier.suggest(text)
                print(
                    f"  {label:9} score={result['score']:.3f} {result['hashtag']!s:16} {text}"
                )
