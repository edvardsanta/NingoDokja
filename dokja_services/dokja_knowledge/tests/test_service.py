import pytest

from fakes import FakeEmbedder
from service import KnowledgeService
from store import Store

KANT = ("Kant e a razao pura",
        "A Critica da Razao Pura de Immanuel Kant examina os limites do conhecimento humano. "
        "Kant distingue fenomeno e coisa em si, e defende que a mente organiza a experiencia "
        "por meio de categorias como espaco, tempo e causalidade.")
BOLO = ("Bolo de cenoura",
        "Para o bolo de cenoura, bata no liquidificador as cenouras, os ovos e o oleo. "
        "Misture a farinha e o acucar, asse por quarenta minutos e cubra com calda de chocolate.")
JUROS = ("Juros e inflacao",
         "Quando o banco central sobe a taxa Selic, o credito encarece e a inflacao tende a desacelerar. "
         "O cambio e as expectativas de mercado tambem pressionam os precos ao consumidor.")


@pytest.fixture
def embedder():
    return FakeEmbedder()


@pytest.fixture
def service(embedder):
    return KnowledgeService(Store(":memory:"), embedder=embedder, model="fake")


def add(service, doc, **extra):
    title, body = doc
    return service.ingest({"title": title, "body": body, **extra})


# ---- ingest -------------------------------------------------------------------------

def test_ingest_stores_chunks_with_embeddings_and_a_default_source_id(service):
    result = add(service, KANT, tags=["filosofia", " kant ", "filosofia"])

    assert result["source_id"] == "note:kant-e-a-razao-pura"
    assert result["created"] and result["changed"] and not result["degraded"]
    assert result["chunks"] >= 1 and result["embedded"] == result["chunks"]
    listed = service.list_documents({})["documents"][0]
    assert listed["tags"] == ["filosofia", "kant"]
    assert listed["chunks"] == listed["embedded"]


def test_ingesting_the_same_content_again_is_a_no_op(service, embedder):
    add(service, KANT)
    calls = len(embedder.calls)

    again = add(service, KANT)

    assert again["changed"] is False and again["created"] is False
    assert len(embedder.calls) == calls, "unchanged content must not be embedded again"


def test_editing_a_document_replaces_its_chunks_and_its_keyword_index(service):
    add(service, KANT, source_id="kant")
    edited = service.ingest({"source_id": "kant", "title": "Kant", "body": "Agora o texto fala so de estoicismo e Marco Aurelio."})

    assert edited["changed"] and not edited["created"]
    assert service.list_documents({})["total"] == 1
    old = service.search({"query": "categorias espaco causalidade fenomeno"})
    assert all(hit["source_id"] != "kant" or "estoicismo" in hit["text"] for hit in old["hits"])
    assert not any(hit["relevant"] for hit in old["hits"]), "stale text must not stay searchable"
    new = service.search({"query": "estoicismo Marco Aurelio"})
    assert new["hits"][0]["source_id"] == "kant" and new["hits"][0]["relevant"]


@pytest.mark.parametrize("payload, message", [
    ({"title": "", "body": "x"}, "title"),
    ({"title": "T", "body": "   "}, "body"),
    ({"title": "T", "body": "x", "source_id": "bad id with spaces"}, "source_id"),
    ({"title": "T", "body": "x", "kind": "Not Valid!"}, "kind"),
    ({"title": "T", "body": "x", "tags": [f"t{i}" for i in range(25)]}, "tags"),
    ({"title": "T", "body": "# so titulo\n"}, "no text"),
    ({"title": "x" * 400, "body": "x"}, "title"),
])
def test_ingest_validates_its_input(service, payload, message):
    with pytest.raises(ValueError, match=message):
        service.ingest(payload)
    assert service.list_documents({})["total"] == 0


def test_delete_removes_the_document_and_its_keyword_matches(service):
    add(service, BOLO)
    assert service.search({"query": "bolo de cenoura"})["relevant_count"] >= 1

    assert service.delete({"source_id": "note:bolo-de-cenoura"})["deleted"] is True
    assert service.delete({"source_id": "note:bolo-de-cenoura"})["deleted"] is False

    result = service.search({"query": "bolo de cenoura"})
    assert result["hits"] == []
    assert service.status()["chunks"] == 0
    with pytest.raises(ValueError):
        service.delete({})


# ---- retrieval (the failure the OpenHuman spike had) -----------------------------------

def test_each_query_finds_its_own_document_first_and_an_unrelated_one_finds_nothing(service):
    for doc in (KANT, BOLO, JUROS):
        add(service, doc)

    for query, expected in [
        ("o que Kant defende sobre o conhecimento e a razao", "note:kant-e-a-razao-pura"),
        ("como fazer bolo de cenoura com chocolate", "note:bolo-de-cenoura"),
        ("Selic sobe e a inflacao desacelera no credito", "note:juros-e-inflacao"),
    ]:
        result = service.search({"query": query})
        assert result["hits"][0]["source_id"] == expected, query
        assert result["hits"][0]["relevant"] is True, query
        others = [h for h in result["hits"][1:] if h["source_id"] != expected]
        assert all(h["score"] < result["hits"][0]["score"] for h in others), "scores must differ by relevance"

    unrelated = service.search({"query": "escalacao do time de futebol no campeonato"})
    top_scores = [hit["score"] for hit in unrelated["hits"]]
    assert unrelated["relevant_count"] == 0, "nothing in the base matches this"
    assert max(top_scores, default=0.0) < unrelated["threshold"]


def test_scores_are_not_constant_across_queries(service):
    for doc in (KANT, BOLO, JUROS):
        add(service, doc)
    a = service.search({"query": "razao pura de Kant"})["hits"][0]["score"]
    b = service.search({"query": "bolo de cenoura"})["hits"][0]["score"]
    c = service.search({"query": "futebol e campeonato"})["hits"]
    assert a != b
    assert not c or c[0]["score"] < min(a, b)


def test_the_relevance_threshold_can_be_set_per_request(service):
    add(service, KANT)
    query = {"query": "Kant conhecimento humano"}
    assert service.search({**query, "min_score": 0.05})["hits"][0]["relevant"] is True
    strict = service.search({**query, "min_score": 0.99})
    assert strict["threshold"] == 0.99
    # A cosine score cannot pass 0.99 here, but chunks holding the query's words still count.
    assert strict["hits"][0]["coverage"] == 1.0 and strict["hits"][0]["relevant"] is True
    assert service.search({"query": "futebol campeonato", "min_score": 0.99})["relevant_count"] == 0


def test_queries_with_search_syntax_do_not_break_the_keyword_index(service):
    add(service, KANT)
    for query in ['"Kant"', "kant AND", "kant OR (razao", "razao*", "kant: razao -pura", "'; DROP TABLE chunks;--", "***"]:
        result = service.search({"query": query})
        assert "hits" in result


def test_search_validates_its_input(service):
    with pytest.raises(ValueError):
        service.search({"query": "   "})
    with pytest.raises(ValueError):
        service.search({"query": "x" * 3000})


# ---- degraded mode --------------------------------------------------------------------

def test_ingest_still_works_when_the_embedding_server_is_down(service, embedder):
    embedder.down = True

    result = add(service, KANT)

    assert result["degraded"] is True and result["embedded"] == 0
    assert "unreachable" in result["reason"]
    assert service.status()["pending_embeddings"] == result["chunks"]


def test_search_falls_back_to_keywords_and_says_so(service, embedder):
    add(service, KANT)
    add(service, BOLO)
    embedder.down = True

    result = service.search({"query": "categorias espaco tempo causalidade"})

    assert result["degraded"] is True and "keyword search only" in result["reason"]
    assert result["hits"][0]["source_id"] == "note:kant-e-a-razao-pura"
    assert result["hits"][0]["score"] is None
    assert result["hits"][0]["relevant"] is True, "exact words in the chunk still count"
    unrelated = service.search({"query": "escalacao futebol campeonato"})
    assert unrelated["relevant_count"] == 0


def test_reindex_embeds_what_was_missed_once_the_server_is_back(service, embedder):
    embedder.down = True
    added = add(service, KANT)
    embedder.down = False

    result = service.reindex({})

    assert result["embedded"] == added["chunks"] and result["remaining"] == 0
    assert service.search({"query": "Kant razao pura"})["degraded"] is False


def test_a_service_without_any_embedder_is_keyword_only():
    service = KnowledgeService(Store(":memory:"), embedder=None, model="none")
    add(service, KANT)
    result = service.search({"query": "kant razao"})
    assert result["degraded"] is True and "no embedding server" in result["reason"]
    assert service.status()["embedder_reachable"] is None
    assert service.reindex({})["degraded"] is True


def test_embeddings_from_another_model_are_not_used(embedder):
    store = Store(":memory:")
    old = KnowledgeService(store, embedder=embedder, model="old")
    added = add(old, KANT)
    new = KnowledgeService(store, embedder=embedder, model="new")

    assert old.status()["embedded"] == added["chunks"]
    assert new.status()["embedded"] == 0 and new.status()["pending_embeddings"] == added["chunks"]
    assert all(hit["score"] is None or hit["score"] == 0 for hit in new.search({"query": "kant"})["hits"])

    assert new.reindex({})["remaining"] == 0
    assert new.status()["embedded"] == added["chunks"]
    assert new.search({"query": "Kant razao pura"})["hits"][0]["score"] > 0.3


# ---- misc -----------------------------------------------------------------------------

def test_status_and_dispatch(service):
    add(service, KANT)
    status = service.dispatch({"type": "knowledge.status"})
    assert status["documents"] == 1 and status["embed_model"] == "fake" and status["embedder_reachable"] is True
    assert service.dispatch({"type": "knowledge.list", "payload": {"limit": 1}})["total"] == 1
    with pytest.raises(ValueError, match="unsupported"):
        service.dispatch({"type": "knowledge.nope"})
    with pytest.raises(ValueError):
        service.dispatch({"type": "memory.fetch"})


def test_list_documents_pages_newest_first(service):
    for index in range(5):
        service.ingest({"title": f"Nota {index}", "body": f"conteudo numero {index} distinto"})
    first = service.list_documents({"limit": 2})
    second = service.list_documents({"limit": 2, "offset": 2})
    assert first["total"] == 5 and len(first["documents"]) == 2
    assert not {d["source_id"] for d in first["documents"]} & {d["source_id"] for d in second["documents"]}
