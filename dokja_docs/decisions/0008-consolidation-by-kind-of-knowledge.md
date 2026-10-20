# ADR 0008: consolidate knowledge by its kind, tagged at reading time

Date: 2026-10-19. Status: proposed; owner answers recorded, kinds and classifier under discussion.
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

Treat four axes separately:

| Axis | Examples | Decides | Owner |
| --- | --- | --- | --- |
| Source and format | PDF, EPUB, feed item, page | How to read | Reading host (step 07) |
| Kind of knowledge | news, book; later argument, technical reference, narrative | How to consolidate, and which summary method applies | Knowledge (methods are data) |
| Goal | study, stay informed, quick reference | Which part of the method matters | The request |
| Experience | "a technical book read to study: per-chapter concepts worked" | Which way of applying the method to choose | Experience (Memory) |

**The kind is classified at reading time and stored as a tag.** A source can
carry a classifier, as a reading-host plugin next to its extractor. Its output
is a `kind:<name>` tag on the stored knowledge. Consolidation, retrieval and
summary methods select on that tag. The source alone never fixes the method; it
only proposes the kind.

Proposed order of precedence for the kind:

1. the user's explicit kind;
2. the source's classifier;
3. the source's default (a feed item is news, an EPUB is a book);
4. `general`.

The tag records which of these decided it, so experience can later judge the
classifier.

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
  - Output: `kind:` tags, defining tags (topics, entities, a date anchor for
    news) and its own `id@version`.
  - It is selected and replaced like an extractor: manifests, no code.
- **Precedence:**
  1. the user's explicit kind;
  2. the classifier;
  3. the source's default (a feed item is `news`, an EPUB is `book`);
  4. `general`.

  The stored tags record which of these decided (`kind-by:user`, and so on).
- **Determinism:** tags are part of Knowledge's content hash, so the same
  input must give the same tags, or the document is re-chunked and
  re-embedded on every read. A model-based classifier records its version, and
  a new version re-tags explicitly.
- **First implementation:** source defaults plus deterministic rules (format,
  feed origin, simple structure cues). A model, or the local agent CLI, can
  replace it later through the same capability, judged by experience.
