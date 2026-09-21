# dokja_knowledge

Base de conhecimento de pesquisa: guarda documentos que o usuário alimenta e
os recupera por significado e por palavra-chave, com fonte citável.

- Armazenamento: SQLite próprio (`KNOWLEDGE_DB_FILE`), separado do `dokja.db`.
- Recuperação híbrida: cosseno sobre embeddings (bge-m3 via Ollama) + FTS5,
  fundidos por reciprocal rank fusion.
- Sem Ollama (ou logo após trocar o modelo, antes do reindex) o serviço
  continua de pé: ingere e busca só por palavra-chave (`degraded: true` +
  `reason`). `knowledge.reindex` embute o que faltou.
- Reenviar o mesmo conteúdo não faz nada (hash); editar substitui os chunks.

## Eventos (ZeroMQ REQ/REP, porta 5561)

| evento | payload | resultado |
| --- | --- | --- |
| `knowledge.ingest` | `title`, `body`, `kind?`, `source_id?`, `source_ref?`, `tags?` | `created`, `changed`, `chunks`, `embedded`, `degraded` |
| `knowledge.search` | `query`, `k?` (1–20), `min_score?` | `hits[]`, `relevant_count`, `threshold`, `degraded` |
| `knowledge.list` | `limit?`, `offset?` | `documents[]`, `total` |
| `knowledge.delete` | `source_id` | `deleted` |
| `knowledge.status` | — | contagens, `embed_model`, `embedder_reachable` |
| `knowledge.reindex` | `limit?` | `embedded`, `remaining` |

## Relevância

`search` sempre devolve os melhores candidatos; **quem injeta contexto em um
prompt deve usar só os hits com `relevant: true`**. `relevant` é um sinal
absoluto: cosseno >= limiar, ou o trecho contém >= 2 termos da pergunta e
>= 75% deles. Ordem no ranking não prova relevância.

### Limiar (`KNOWLEDGE_MIN_SCORE`, padrão 0.45) — provisório

Medido com bge-m3 em apenas 3 documentos (Kant, bolo, juros):

| tipo de pergunta | maior cosseno |
| --- | --- |
| paráfrase sem palavras em comum | 0.46 – 0.61 |
| pergunta com as palavras do texto | 0.63 – 0.73 |
| assunto vizinho ("receita de lasanha", "o que é um imposto") | 0.43 – 0.45 |
| sem relação (futebol, nginx, piada, capital da França) | 0.27 – 0.41 |

Esses grupos quase se encostam (a paráfrase mais fraca fez 0.456; o vizinho
mais forte, 0.446). Trate 0.45 como ponto de partida, não como calibração:
uma base maior muda a distribuição. Por isso o consumidor deve sempre citar a
fonte de cada trecho usado, para que uma recuperação errada seja visível. O
teste vivo (`tests/test_live_embedder.py`, pulado sem Ollama) inclui os casos
vizinhos e imprime a distribuição.

## Variáveis

`KNOWLEDGE_SERVICE_ENDPOINT`, `KNOWLEDGE_DB_FILE`, `DOKJA_EMBED_ENDPOINT`
(padrão `http://127.0.0.1:11434`), `DOKJA_EMBED_MODEL` (padrão `bge-m3`),
`DOKJA_EMBED=off` (só palavra-chave), `DOKJA_EMBED_TIMEOUT`,
`KNOWLEDGE_MIN_SCORE`.

Trocar `DOKJA_EMBED_MODEL` invalida os vetores antigos (marcados por modelo);
rode `knowledge.reindex`.

## Testes

```sh
pip install numpy pyzmq pytest
python -m pytest dokja_services/dokja_knowledge/tests
```
