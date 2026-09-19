"""Retrieval quality against a real bge-m3. Skipped when no Ollama server is reachable.

The corpus and the questions are in Portuguese on purpose: the threshold in DEFAULT_MIN_SCORE was
measured on them, and they check that retrieval works across languages. Do not translate them
without measuring the scores again.
"""

import pytest

from embedder import OllamaEmbedder
from service import DEFAULT_MIN_SCORE, KnowledgeService
from store import Store

DOCS = {
    "kant": ("Kant e a razão pura",
             "A Crítica da Razão Pura, de Immanuel Kant, examina os limites do conhecimento humano. "
             "Kant distingue o fenômeno da coisa em si e defende que a mente organiza a experiência "
             "por meio de categorias, como espaço, tempo e causalidade."),
    "bolo": ("Bolo de cenoura",
             "Para fazer o bolo de cenoura, bata no liquidificador as cenouras, os ovos e o óleo. "
             "Misture a farinha e o açúcar, asse por quarenta minutos e cubra com calda de chocolate."),
    "juros": ("Juros e inflação",
              "Quando o banco central sobe a taxa Selic, o crédito encarece e a inflação tende a "
              "desacelerar. O câmbio e as expectativas de mercado também pressionam os preços."),
}
# Paraphrases: they share (almost) no words with the document they must find.
PARAPHRASES = {
    "kant": "de que forma a mente molda aquilo que percebemos do mundo?",
    "bolo": "receita de sobremesa doce feita com raiz alaranjada",
    "juros": "por que aumentar o custo do dinheiro emprestado segura o aumento dos preços?",
}
UNRELATED = [
    "quem ganhou o campeonato de futebol ontem?",
    "como configurar um servidor nginx com https?",
    # Neighbouring topics: the hard cases that actually pin the threshold.
    "receita de lasanha",
    "explica o que é um imposto",
]


@pytest.fixture(scope="module")
def live():
    embedder = OllamaEmbedder(timeout=120)
    if not embedder.reachable():
        pytest.skip("Ollama is not running")
    service = KnowledgeService(Store(":memory:"), embedder=embedder, model="bge-m3")
    for key, (title, body) in DOCS.items():
        result = service.ingest({"title": title, "body": body, "source_id": key})
        if result["degraded"]:
            pytest.skip(f"bge-m3 is not usable: {result.get('reason')}")
    return service


@pytest.mark.parametrize("key", list(PARAPHRASES))
def test_a_paraphrase_finds_its_document_without_sharing_words(live, key):
    result = live.search({"query": PARAPHRASES[key]})
    assert result["degraded"] is False
    assert result["hits"][0]["source_id"] == key
    assert result["hits"][0]["relevant"], f"score {result['hits'][0]['score']} under {result['threshold']}"


@pytest.mark.parametrize("query", UNRELATED)
def test_unrelated_questions_stay_under_the_threshold(live, query):
    result = live.search({"query": query})
    assert result["relevant_count"] == 0, [(h["source_id"], h["score"]) for h in result["hits"]]


def test_print_score_distribution(live, capsys):
    """Not an assertion: shows the numbers behind DEFAULT_MIN_SCORE."""
    with capsys.disabled():
        print(f"\nDEFAULT_MIN_SCORE={DEFAULT_MIN_SCORE}")
        for label, queries in (("related", PARAPHRASES.values()), ("unrelated", UNRELATED)):
            for query in queries:
                hit = live.search({"query": query})["hits"][0]
                print(f"  {label:9} top={hit['score']:.3f} {hit['source_id']:5} {query}")
