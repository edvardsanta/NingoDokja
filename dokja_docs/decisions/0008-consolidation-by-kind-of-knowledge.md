# ADR 0008: consolidate knowledge by form and content kind

Date: 2026-10-19. Status: form/content axes and structured metadata accepted;
kinds accepted, agent CLI allowed; Book retirement scope and remaining provider/migration details open.
Refines [ADR 0005](./0005-knowledge-experience-content.md) and
[ADR 0006](./0006-reading-macro-component.md). Replaces the `book.*` facade
planned for step `09`.
Related: [decision index](./README.md), [step 08 evaluation](../step-08-evaluation.md).

## Context

Planning the `book.*` facade showed that a Book-specific flow is the wrong unit:

- "Book" is not a service (ADR 0006).
- A summary only makes sense as a capability for any knowledge. Its method,
  though, depends on what that knowledge is. A news item and a book are read,
  kept and summarized differently.
- The reading experience also differs. For news it is "was I informed in time,
  without repetition". For a book it is "did I understand it, can I find it
  again".

ADR 0005 already separates source type (how to read) from the general method
(how to apply knowledge). It does not name the axis that selects the method.

## Decision

Treat these axes separately:

| Axis | Examples | Decides | Owner |
| --- | --- | --- | --- |
| Source and format | PDF, EPUB, feed item, page | How to read | Reading host (step 07) |
| Form | book, article, note, conversation, meme | The document's structure and attribution | Knowledge |
| Content kind | news, reference, research, narrative | How to consolidate, and which summary method applies | Knowledge (methods are data) |
| Goal | study, stay informed, quick reference | Which part of the method matters | The request |
| Experience | "a technical book read to study: per-chapter concepts worked" | Which way of applying the method to choose | Experience (Memory) |

**Form and content kind are classified at reading time and retained as structured
metadata.** A source can carry a classifier as a reading-host plugin next to its
extractor. The classification, entities, topics, dates, and classifier provenance
are separate from free-form request tags. Consolidation, retrieval, and summary
methods can select on these fields. The source proposes defaults; it does not
dictate the content kind. This supersedes the initial `kind:<name>` tag encoding.

Order of precedence, independently for each classification axis:

1. the user's explicit value;
2. the source's classifier;
3. the source's default (for example, a feed can propose article/news, an EPUB book/general);
4. `general`.

Each axis records which of these decided it, so experience can later judge the
classifier. Choosing the form explicitly must not make the content kind appear
user supplied if that axis came from a classifier.

**Summary is one capability with methods per kind.** One generator (the
language faculty, still open as chat-plan decision #1) applies a method chosen
by kind and goal. The methods are data in Knowledge, for example:

- news: what happened, when, and why it matters;
- argument: thesis, arguments, objections;
- book: per-chapter outline and concepts.

Experience records which method was applied to which kind and goal, and the
outcome. A summary is generated on request and not stored.

## Consequences

- No `book.*` facade with Book-specific logic. `book.*` and the CLI `book`
  command become aliases of the generic flow, or are removed (open question 5).
- The Book service, domain, client and `DOKJA_BOOK_*` settings are retired once
  the generic flow covers their use, with the owner's approval.
- Feeds collection (news) and on-demand reading (books) feed the same
  consolidation, distinguished by the `kind:` tag, not by their source.

## Owner answers (2026-10-19)

| # | Question | Answer |
| --- | --- | --- |
| 1 | Which kinds | Think beyond news and book; see the proposed set below |
| 2 | Classifier contract | Still being discussed; see the proposal below |
| 3 | What consolidation keeps | News does not expire. It is specific entities in a context anchored to a day, month and year. |
| 4 | Outcome of a reading experience | Experience is knowing **how to talk about** each piece of retained knowledge: the way to express and discuss it depends on its kind |
| 5 | `book.*` and the CLI | Remove the CLI `book` command |
| 6 | Tag budget | As many tags as are needed to define the knowledge |

### Consequences of the answers

- **News is temporal, not perishable.** It is consolidated with:
  - a date anchor (day, month and year it refers to);
  - the entities involved;
  - the context that links them.

  It stays retrievable by date and entity; nothing expires.
- **Experience is about expression.** What experience learns is how to speak
  about a kind of knowledge: register, depth, structure, what to cite. This
  moves the feedback signal into conversation: whether the way Ningo talked
  about something worked. It links this record to the chat plan (chat as a
  faculty) and to the hashtag finding of step 08 that an explicit tag alone
  produced no feedback.
- **Tags are not capped at a small number.** Knowledge allows 20 tags per
  document today. Either that limit is raised, or the defining tags (`kind:`,
  topics, entities, date) become their own structured fields, apart from
  free-form request tags. This needs a schema review, which ADR 0003 requires
  for a new store field.
- **Removing the CLI `book` command** is a breaking change to an interface
  command and is approved for that command. Retiring the `book.*` routes, the
  Book domain and the Book service follows the same direction (ADR 0006), and
  is done in the same change only if the owner confirms.

## Proposal for question 1: kinds of knowledge

Each kind says what consolidation keeps and how Ningo talks about it. The
second column is what experience refines.

| Kind | Consolidation keeps | How to talk about it | Typical sources |
| --- | --- | --- | --- |
| `news` | Entities, a date anchor (day, month and year), context, the claim and its source | Situate it in time, name who and what, separate fact from claim | Feeds, pages |
| `book` | Structure (chapters), concepts and terms, the main argument or arc | By chapter or concept, with depth on request | EPUB, PDF |
| `article` | Thesis, arguments, objections, conclusion | Argue with it: agree, object, compare | Pages, PDFs |
| `reference` | Definitions, procedures, parameters, versions | Precise and literal, quote the exact term or step | Documentation, manuals |
| `research` | Question, method, evidence, findings, limits | Cite the evidence and its limits; do not overstate | Papers |
| `narrative` | Characters, events, setting, themes | Retell and interpret, without spoiling unless asked | Fiction, stories |
| `note` | The owner's own words, kept as written | As the owner's view, never as an outside fact | The owner's notes |
| `conversation` | Who said what, when, and what was agreed | Attribute to the speaker; do not merge voices | Chat history (WhatsApp, Discord) |
| `meme` | The text, the labelled hashtag, the humor's reference | Play along; never explain it as fact | The meme pool (labelled examples today) |

`book` and `article` describe form, while `narrative`, `research` and
`reference` describe content. A book can be narrative or reference. The kinds
may need two tags (form and content) instead of one; that is part of
question 2.

## Proposal for question 2: the classifier

- **Where:** a reading-host plugin with its own capability,
  `document.classify.v1`, next to `document.extract.v1`.
  - Input: the extracted documents and the source's provenance.
  - Output: structured form/content proposals and defining metadata (topics,
    entities, a date anchor for news), with its own `id@version`.
  - It is selected and replaced like an extractor: manifests, no code.
- **Precedence (per axis):**
  1. the user's explicit kind;
  2. the classifier;
  3. the source's default (a feed item is `news`, an EPUB is `book`);
  4. `general`.

  The structured provenance records which of these decided each value.
- **Determinism:** tags are part of Knowledge's content hash, so the same
  input must give the same tags, or the document is re-chunked and
  re-embedded on every read. A model-based classifier records its version, and
  a new version re-tags explicitly.
- **First implementation:** source defaults plus deterministic rules (format,
  feed origin, simple structure cues). A model, or the local agent CLI, can
  replace it later through the same capability, judged by experience.

## Owner direction on agents and kinds (2026-10-19)

- **The proposed kinds are accepted** as the starting set. Whether a document
  carries one kind or separate form/content values was subsequently resolved:
  keep both, as recorded below.
- **Local agent CLIs may do the judgment work.** Claude Code and Codex, run
  locally, may classify kinds, extract defining tags (topics, entities, the
  date anchor of news), and generate summaries and other text. They are
  providers behind the existing capabilities (`document.classify.v1` for
  classification, the summary capability for generation), not new routes, so
  they are replaced like any plugin.

### How an agent classifier stays deterministic

Tags are part of Knowledge's content hash, and a model does not answer the same
way twice. So:

- Classification runs once per content and classifier version. The result is
  kept, keyed by the content hash and `classifier@version`. Reading the same
  file again reuses it instead of asking the agent again.
- A new classifier version re-tags only when asked explicitly, never as a side
  effect of a re-read.
- The stored tags record the classifier and its version (`kind-by:`), so
  experience can compare classifiers later.

### Constraints that come with an agent CLI

- **Latency.** A call takes seconds, so it runs off delivery paths. Reading on
  demand and summaries on request can wait; a scheduled meme delivery cannot
  (the 1 s shadow-prediction cap accepted in step 08 stays).
- **Provider.** The text read is sent to the CLI's provider. Owner notes and
  chat history (`form=note`, `form=conversation`) are the owner's private material.
  A setting decides which inputs may be sent, and the default excludes those
  forms even when their content kind is research, reference, or another value.
  Evaluate that restriction before sending material to a classifier; its own
  output cannot establish permission to send its input.
- **Bounds.** The call runs under the reading host's subprocess limits:
  - its timeout;
  - an output limit;
  - only declared variables;
  - output validated against the contract (allowed kinds, tag syntax and
    count).

  If the agent fails, the rule-based default (source default, then `general`)
  is used and recorded.

## Owner decisions on representation (2026-10-19)

The owner selected **structured metadata separate from free-form tags** and,
after reviewing examples, **both form and content kind**. A book can contain
research, reference material, or narrative; a research method should not depend
on whether its input is a book, article, or supplied note.

Initial domain vocabulary:

| Axis | Values |
| --- | --- |
| Form | `book`, `article`, `note`, `conversation`, `meme`, `general` |
| Content kind | `news`, `reference`, `research`, `narrative`, `general` |

File formats such as PDF and EPUB remain source/reader provenance. Topics such
as physics remain defining metadata, not additional form/content kinds. Each
axis supports an unspecified proposal and an explicit `general` value; those
have different precedence semantics.

The first implementation is the internal
[Knowledge classification contract](../../dokja_domain/dokja_knowledge/classification.go):
user values win, then valid versioned classifier proposals, then source defaults,
then `general`, evaluated independently per axis. Invalid explicit values or
selected source defaults are rejected. An invalid classifier axis falls back
without discarding the other valid axis; a proposal needs provider ID/version
before it can be attributed. Diagnostics use fixed reason codes.

This contract does not classify text, invoke a provider, select a summary method,
or persist metadata yet. It also does not redefine existing public `kind` values
such as `meme_hashtag`, whose isolation semantics remain in force.

Persistence follows in step `09.2`, including additive schema and rollback review,
legacy-document behavior, classification reuse by content/classifier version,
and metadata updates that avoid unnecessary re-embedding. Earlier sections that
describe tags participating in the content hash characterize the current service
and original proposal; they do not require re-embedding content on every metadata
change. Reading-host integration and provider failure behavior are separate work.
