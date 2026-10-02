# Experience Memory Plan

Status: implemented (see "Delivery sequence"). The thresholds are provisional and nothing has
been calibrated against real data yet; the first consumer runs in shadow mode.

This document replaces the earlier plan of the same name (commit `2733028`, a ledger of
operator-recorded analyses). The rules that plan set and this one drops or changes are listed
in "What changed from the earlier plan", so the repository holds one design, not two.

## Objective

The bot acts: it speaks, reads, writes, listens and predicts. To choose well it needs a loop:

```
experience → memory → similar earlier experiences → prediction → choice of behaviour → new experience
```

An **experience** is "in this context the bot took this action, and this was the outcome". The
experience memory records them, finds the closest earlier ones, and lets the bot estimate how
likely an action is to be accepted before it acts. It is not the research knowledge base
([dokja_knowledge](../dokja_services/dokja_knowledge/README.md) stores documents) and it does
not train or host a language model.

## Responsibility boundaries

Follow `Interface → Event/Request → Orchestrator → Domain → Service`.

| Layer | What it owns |
| --- | --- |
| Service `dokja_services/dokja_memory` (Go) | Storage, embeddings and nearest-neighbour recall. No business rules. Its own SQLite file, readable by its owner only, because a context is the user's own text. Events are documented in its [README](../dokja_services/dokja_memory/README.md) |
| Domain `dokja_domain/dokja_memory` (Go, pure) | Request validation, the prediction and its scoring. No IO |
| Orchestrator | The `memory.*` events: one workflow action per event, routed to the domain; `memory` can be switched off like any service. Calls made by an action are best effort with a short time budget: an unreachable memory must never stop a reply or a meme dispatch |
| CLI | `dokja-cli memory` to predict, recall, list, score, record, resolve and forget |
| TUI | `m` on the Painel: the memory's state and how its predictions fare against the baseline (one key embeds a batch of experiences that have no vector), a browsable list of experiences with `l` (filter, page, forget after a confirmation), a prediction form with `p`, and, on the Memes tab, the chance a suggestion is kept next to the one `g` shows. Read on demand and kept out of the panel refresh, because a stopped service takes seconds to fail and must not stall the panel; the chance after `g` is a second request that never costs the suggestion anything |

The service runs as its own process so that recording an experience on the path of an action
never waits behind a slow document ingest or a reindex in another service.

## Prediction and scoring

- **Predict.** For an action in a context, take the similar earlier experiences of that action
  that have a verdict, weight them by similarity and pull the result toward the **baseline**,
  the action's overall acceptance rate. With too little evidence, or no similarity (the
  embedder is down), the prediction is the baseline and says so. A prediction never invents
  confidence: it reports its support, the number of resolved experiences and why it was
  insufficient.
- **Outcomes.** `accepted` is the only success; `replaced` and `ignored` are failures;
  `expired` is no verdict (nobody resolved it within `MEMORY_EXPIRE_AFTER`) and is left out of
  every count that predicts or scores. Expiry is a view, not a write, so a late verdict still
  counts. The first verdict wins.
- **Score.** The prediction and the baseline are stored with the experience at the moment of
  the decision and never recomputed. The Brier score compares both against what happened, only
  on resolved experiences that carry a stored prediction, and reports the sample size and
  whether there is enough data to judge. A prediction is only said to beat the baseline when
  its skill is above zero.
- The thresholds are provisional. They are deliberately not borrowed from the knowledge base
  (calibrated on three long documents; a context here is a short message) and need calibrating
  against real data.

## First consumer

The suggestion of a hashtag for a meme (`meme.hashtag.suggest`, learned by example) already has
the shape of the loop: a context (the text read from the image), an action (the suggested tag)
and, later, a verdict (the operator keeps or replaces it).

1. **Shadow mode.** When a suggestion is made, predict the chance it is accepted and record the
   experience with that prediction and with what the suggestion rested on (how close the earlier tagged
   meme was and the start of its text). When the operator tags the same meme, resolve it as
   `accepted` or `replaced`. Nothing changes for the operator; only the score accumulates.
2. **Deciding.** Once there is data and the score beats the baseline, the prediction gates the
   behaviour (append the tag or not). That is the "choice of behaviour" step of the loop.

A suggestion and the tag that follows it meet at the meme's address, hashed into the
experience's `ref`: the address is what the pool stores, what a scheduled delivery attaches and
what the terminal interface tags with, and the meme service keeps a tag's address as it came. A
tag typed with a different form of the address (a re-uploaded copy, say) does not match: that
suggestion stays pending, expires and is left out of the score, which is the safe way to be
wrong. A tag for a meme the bot never suggested a hashtag for finds no experience and changes
nothing.

## What changed from the earlier plan

| Earlier rule | Now |
| --- | --- |
| Experiences persist through a repository port on `dokja_store` and its migrations | **Dropped.** A dedicated service with its own SQLite file, so it can record on the path of an action without going through the orchestrator's store, and be exported or wiped on its own. `dokja.db` stays as it is |
| Prediction scoring belongs to a separate prediction domain | **Changed.** The only prediction today is the chance an action is accepted, so it lives in the memory domain. Split it out when predictions about analyses return |
| Records keep evidence snapshots (exact excerpt, hash, capture time) | **Partly changed.** An action's context is the input the bot actually received and is immutable, so it needs no snapshot. What a choice rested on is kept: a hashtag suggestion records how close the earlier tagged meme was (`matched_score`) and the start of its text (`matched_context`, 200 characters at most), so the memory can say why a tag was suggested. Hash and capture time stay dropped; they are needed again only if analyses are recorded |
| Revisions, explicit scope, separate occurrence and recording times | **Dropped for now.** An experience is immutable and has one resolution time. Same condition as above |
| A pending experience may be recalled, never shown as success or failure | **Changed.** Pending ones are left out of neighbours and counts because they carry no label; they are counted in `memory.status`. None is ever presented as a success or a failure |
| Recall goes through the knowledge index, then resolves the authoritative record | **Dropped.** The service keeps its own embeddings. The knowledge index stays for documents |
| First release: analyses and predictions recorded by the operator | **Changed.** First release: outcomes of the bot's own actions, starting with hashtag suggestions. Operator-recorded analyses with evidence are deferred, not rejected |
| A local language model enriches each record through a durable worker | **Dropped from memory.** Memory needs no language model. The hardware notes and model candidates of the earlier plan (GPUs, a 7-8 billion parameter model at four bits, a Vulkan backend to try first) still apply to the text-generation replacement for `chat_ai`, which is a separate track |
| Automatic capture of conversations and autonomous outcome collection are out of scope | **Partly changed.** Recording the outcome of a bot action is automatic by design. Conversations are not captured |
| New variables, volumes or services update the compose files and the production runbook together | **Kept** |
| Duplicate capture is idempotent; unknown stays unknown; Brier only with explicit probabilities and with sample sizes | **Kept.** A repeated `ref` changes nothing; a prediction is optional; the scorecard counts unscored experiences and says when there is not enough data |
| Contexts are the user's own text; private conversations do not enter a shared corpus | **Kept, with one deliberate exception.** Requests are logged by type and field count only, the database file is owner-only, nothing is shared. A context comes back in one case: the first 160 characters, as a snippet, when a listing, a recall or a prediction's evidence is asked for with `include_context`, because the ref of an experience is a hash and an operator cannot recognize one without a hint of what it was about. The cut is made in the service, so the rest never leaves it, and `memory.get` and `memory.resolved` never return a context. The text of the earlier example a choice rested on follows the same rule: `matched_snippet`, only with `include_context`. Like `knowledge.search`, the answer crosses the orchestrator port, which has no authentication: keep that port off untrusted networks |
| No model training | **Kept.** A learned model is deferred until there are thousands of resolved experiences and the neighbour method stops improving on the baseline |

## Delivery sequence

| Stage | Deliverable | State |
| --- | --- | --- |
| 1 | Memory domain: validation, prediction, scoring | Implemented |
| 2 | Memory service: storage, embeddings, recall | Implemented |
| 3 | Orchestrator: `memory.*` routes, handler, client, service switch | Implemented |
| 4 | Hashtag suggestions as the first consumer, in shadow mode | Implemented |
| 5 | CLI: `dokja-cli memory` | Implemented |
| 6 | TUI: the memory view, the experience list, the prediction form and the chance next to `g` | Implemented |
| 7 | The service in the three compose files and the production runbook, added with the change each describes (the service, the orchestrator route, the hashtag use, the CLI and the TUI) | Implemented |

Production deployment remains a separate approval step. The database is one file: copy it while
the service is stopped, or use SQLite's online backup, to back it up or move it. A new embedding
model needs `memory.reindex` until nothing remains.

## Not in scope

Recording analyses or forecasts written by the operator, evidence snapshots, revisions,
conversation capture, citation outcomes in chat, and any learned model. Each can be added on top
of the same experience log when there is a reason to.
