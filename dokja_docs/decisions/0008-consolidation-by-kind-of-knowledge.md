# ADR 0008: consolidate knowledge by its kind, tagged at reading time

Date: 2026-10-19. Status: proposed (owner discussion; open questions below).
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

## Open questions

1. **Which kinds to start with.** Proposed: `news` and `book`, the two
   contrasting cases.
2. **The classifier contract.** A `document.classify.v1` reading-host plugin
   next to `document.extract.v1`, or part of the extractor's output. It must
   be deterministic, or record its version, because tags are part of
   Knowledge's content hash.
3. **What consolidation keeps per kind.** For example:
   - news: a date, entities and the claim with its source, and an expiry;
   - book: structure per chapter and its main topic tags, kept durably.
4. **The outcome of a reading experience.** Without a feedback signal,
   experience learns nothing; step 08 measured this as the bottleneck.
   Candidates:
   - a follow-up question;
   - searching for it again;
   - an explicit rating;
   - reuse in a conversation.
5. **`book.*` and the CLI `book` command.** Keep them as aliases of read →
   consolidate → (summarize on request), or remove them (breaking).
6. **Tag budget.** Knowledge allows 20 tags. Provenance (`format:`, `reader:`),
   the kind and topic tags all share it with the request's own tags.
