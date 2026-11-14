# ADR 0016: knowledge proposals and sub-knowledge ownership

Date: 2026-11-14. Status: accepted.

## Context

Everything Ningo learns lives in Knowledge. The owner decided that a meme is
not a Meme-domain feature but one kind of knowledge record — a *sub-knowledge*:
a specialised record with its own validated shape and rules, sharing the
knowledge lifecycle. The labelled example (`meme_hashtag`) already lived in
Knowledge under ADR 0007; what was missing was the way anything that is not the
owner's own intake becomes knowledge.

Two owner decisions from 2026-11-14 set the boundary:

- Anyone mapped as a known person can send things for Ningo to learn; only the
  owner decides, and the decision happens in the Desktop.
- The mechanism is general: article links, files and meme tags are kinds of one
  proposal flow, not separate features. The meme-channel intake and its reply
  command build on top of it as a consumer.

## Decision

The shared lifecycle of a knowledge record is:

```
intake → (approval, when the source is not the owner) → stored with provenance → retrieval
```

- A **proposal** is a pending claim owned by the Knowledge domain: an id, a
  kind, the kind's validated payload, who proposed and who sent the material,
  and where. It exists only while pending: approval stores the record carrying
  its provenance and removes the proposal; rejection deletes it, keeping
  nothing.
- **Pending is not knowledge.** Proposals are held in a plain table in the
  owner's local database, outside every store chat retrieval reads. Chat
  retrieval, citations and delivery never see a pending item, and Ningo never
  mentions one. Approving, editing or rejecting is never announced.
- **Provenance is assigned by Knowledge, never by the submitter.** A stored
  record says where it came from — `owner`, `group` or `api` — and, for an
  approved group record, the proposer and the source. Records stored before
  provenance existed read as `owner`.
- **Origin precedence per item is owner, then approved group, then source
  API.** A group proposal on an item the owner already stored never changes
  anything on its own; if the owner approves it anyway, that explicit decision
  replaces the record, and the answer says what it replaced. An owner edit
  later replaces a group record.
- **A kind registers only its payload rules and its approval step.** The
  domain decides validity, duplicates and precedence; the orchestrator only
  sequences. Approval reuses the kind's existing ingestion path — a link
  resolves through the web reader, a file re-reads through the reading host,
  a meme tag goes through OCR and the labelled-example store — never a
  parallel one.
- **The owner's own intakes skip approval.** What the owner sends stores
  directly, as it already did; the queue exists for everyone else who is
  known.

## Consequences

- The Meme domain keeps delivery and suggestion; the meme pool and its labels
  are Knowledge records, one sub-knowledge among others (meme-channel intake
  and its reply command are a separate change on top of this mechanism, per
  `dokja_docs/meme-tag-proposals-plan.md`).
- The Desktop gains an owner-only queue; the CLI and TUI do not decide
  proposals.
- Group content remains read for the reply but never becomes knowledge on its
  own — being known lets someone *propose*, never *write*.
- New knowledge-service metadata accepts a validated `provenance` field;
  existing rows are unchanged and read as `owner`.

## Compatibility

Additive throughout: a new SQLite table (migration), new orchestrator actions
under the Knowledge domain, one new metadata field, a new desktop workspace and
three new desktop actions on the allow-list. No envelope, existing route,
service contract, command, schema, environment variable or compose change. An
older build refuses a database from the future, as before.
