# Knowledge proposals: meme tags from the group

Status: implemented on top of the proposals mechanism of
[ADR 0016](decisions/0016-knowledge-proposals.md). The sections below are the design
the owner approved; "As built" lists where the implementation settled a question.

## As built

- **Who proposes:** a known person (`NingoFacts.People`), as ADR 0016 and issues #427 and
  #428 record. A stranger's post is kept nowhere. The owner's own post is held too, and
  they decide it in the desktop like any other. Widening this to every member is one rule in `ProposeMemeTag`.
- **The reply:** a plain reply naming a tag (`#Tag`) to the message that carries the meme,
  not a slash command, so no Discord command is added.
- **The meme channel** is the configured list of meme channels
  (`DISCORD_SCHEDULED_MEME_CHANNEL_ID`); no new variable. It must also be inside
  `DISCORD_ALLOWED_CHANNELS` when that is set.
- **Where it lives:** the kind is `meme_hashtag`, the same identity as the stored example.
  The post rule is Knowledge's (`ParseMemeTagPost`), the authorization is Communications',
  the routing is the orchestrator's.
- **A video keeps its link**, which can expire; the queue shows the text read at intake and
  the link to the source message. A small image is kept as base64.
- **The owner's own tag** now stores `origin: owner`, so retagging a meme a group label
  described replaces it (a test against the real service pins this).
- **Still open:** the safety screening of a proposed meme before the owner's preview, a
  per-meme duplicate check beyond the pending one, and what happens when the poster edits or
  deletes the message.

## Why

Ningo is a group bot on Discord and also the owner's personal assistant. He knows a
meme tag only if the owner created it: the Knowledge store holds the owner's explicit
labels (`meme_hashtag` examples), and Ningo reads that base, never the group's chat.

When a member posts a meme with a new tag in the meme channel (for example
`#SomethingGames` under a link), Ningo cannot learn it. Asked about it, he correctly
says it is not in his archive, and the member's tag is lost. The group already
proposes tags in practice; only the owner can record them, one by one, in the desktop.

## How it works today

1. The owner opens the desktop, Messages, and picks the meme channel.
2. They refresh the memes, ask for a suggestion based on the meme, and create the tag.
   The tag is stored in Knowledge as a labelled example (OCR text plus label).
3. They send the meme to the group. The scheduled flow also pre-selects some memes; the
   owner validates them one by one before they go to Discord.
4. Tags the owner creates take precedence over the tags that come with the meme from
   the source API, which exist only because the API returns them.

## Decisions already taken

- **Anyone in the group can propose a tag; only the owner approves it.** A proposal
  never enters Knowledge, and Ningo never uses it, before approval.
- **Every proposal is traceable:** who sent the meme and who wrote the tag (they can
  be two people), and where and when.
- **Proposals reach Ningo in two ways:** a post in the meme channel that carries a
  `#Hashtag`, and a command in reply to a meme.
- **Approval happens only in the desktop.** Ningo does not tell the group that a
  proposal is pending, nor that it was approved or refused.
- **Approving writes a Knowledge example with `origin: group`,** plus the proposer and
  the channel the meme was sent in.
- **Rejecting deletes the proposal.** Nothing of it is kept.
- **Precedence of a tag's origin: owner, then approved group proposal, then source
  API.** It applies to the same meme, and the owner may edit that meme's tag later.
- **Memes belong to Knowledge.** A meme is a sub-record of knowledge, not a separate
  concept, and the proposal mechanism is general: any intake that fills Ningo's
  knowledge (an article link, a PDF, a meme) goes through the same approval flow. This
  plan wires the meme kind; the mechanism is built so other kinds can follow.
- **One pull request** carries the whole change, as separate commits per layer.

## Non-goals

- No automatic tag creation by Ningo from the group's chat, and no change to how the
  owner tags today.
- No change to how a suggestion is computed, scored or recorded
  ([hashtag experience](hashtag-experience.md)).
- No public acknowledgement of proposals in Discord.
- Wiring article, PDF and other intakes to the proposal flow: the mechanism allows it,
  but this change does it for memes only.

## Design

The flow stays `Interface -> Event -> Orchestrator -> Domain -> Service`.

### 1. Intake (Discord adapter, then orchestrator)

- The adapter already turns channel messages into `message.created` events. For the
  meme channel it adds nothing to the envelope: the event keeps carrying the author,
  channel, message id and attachments.
- The orchestrator routes a message from the configured meme channel to a proposal
  action when it has a `#Hashtag` and either an attachment or a link. A message
  without both is an ordinary message.
- A reply-command (a name in English and in Portuguese, like the other commands)
  carries a tag and refers to a meme message. The referenced message is the meme; the
  command's author wrote the tag, and the original poster is kept as the sender of the
  meme.
- Eligibility (which channels and who counts as "anyone") belongs to the
  Communications domain's inbound policy, not to the adapter.

### 2. What is kept for the media

- **A file (video or image).** A video keeps the link that already works; the proposal
  does not copy it. An image is small enough to keep as base64, so it survives the
  platform link expiring.
- **A link that is not a file** (for example a short-video page). The proposal keeps the
  link. When the owner approves it, the local post reader (a Feeds `social.resolve.v1`
  plugin, as `digest.link.read` already uses) resolves the post to its media, which is
  then downloaded and read by OCR like any other meme. A refusal from the reader
  (`unsupported`, `login_required`, `unavailable`, `no_reader`, `failed`) is shown to
  the owner as the usual fixed reason, and the proposal stays pending.

### 3. Proposal record (Knowledge domain)

Knowledge owns the proposal, as a pending claim about something to learn. It is held
apart from the stored documents and examples, so nothing in chat retrieval can see it.
It is generic: a `kind` and a kind-specific payload, so other intakes can use it later.
The first kind is the meme tag.

| Field | Meaning |
| --- | --- |
| `id` | stable id |
| `kind` | what is proposed; `meme_hashtag` here |
| `tag` | validated label (the same rules as `HashtagExample`) |
| `media` | the file link, the base64 image or the non-file link |
| `meme_by` | who posted the meme (Discord user id and display name) |
| `tag_by` | who wrote the tag |
| `source` | channel id, message id and a permalink of the message |
| `proposed_at` | timestamp |

A proposal exists only while it is pending: approving turns it into a Knowledge example
and removes it; rejecting deletes it. The record of who proposed lives on the example,
not on the proposal. The domain decides validity, duplicates and precedence; the
orchestrator only sequences; the store is a plain table in the owner's local database,
next to `discord_posts`.

Identity uses the people mapping the project already has, so a known member is shown by
name and an unknown one by their Discord display name.

### 4. Approval (desktop)

- Messages gets a proposals queue: the meme preview, the tag, who sent the meme and the
  tag, the channel it was posted in and a link to the source message. The owner can
  approve, edit the tag and approve, or reject.
- Approving goes through the existing tagging path (OCR, then a Knowledge example). The
  example carries `origin: group`, the proposer and the channel the meme was sent in, so
  precedence and audits can tell it from the owner's own. Editing the tag while
  approving keeps `origin: group`.
- The queue is shown only to the owner's interfaces, never in a channel. The CLI and
  TUI do not approve in this change.

### 5. Precedence

For one meme, the tag used for delivery and citations is the first available of: the
owner's label, an approved group label, the source API's tags. The domain owns this rule.

- A label the owner creates or edits later, in the usual tagging flow, has `origin:
  owner` and replaces any group label for that meme.
- A new group proposal for a meme that already has an owner label never replaces it by
  itself. The queue shows the current label next to the proposal. If the owner approves
  it anyway, that is a decision to replace it, and the approved label has `origin: group`.
- An approved group label is replaced, not merged, when the owner edits the meme's tag.

### 6. Silence

Ningo's replies never mention pending proposals. Pending records live outside the store
that chat retrieval reads, and the capability catalog does not describe the queue as
something he can report on. The only visible effect of an approval is that, later,
Ningo knows the tag.

## Compatibility review (before implementing)

- Event envelope: unchanged.
- New orchestrator route and action, and a new command: additive; the command needs its
  English and Portuguese names and locale entries.
- SQLite: a new table in the owner's database (additive migration).
- Knowledge examples gain `origin`, proposer and channel metadata: additive, absent on
  existing examples and read as `owner`.
- Desktop: a new Messages queue and new request types; existing screens unchanged.
- Env vars and compose: the meme channel id is already configured; no change expected.
  If a new variable is needed, the compose files and the runbook change in the same PR.

## Delivery

A single pull request, in commits that follow the layers:

1. the Knowledge domain: proposal, example `origin` and the precedence rule, with tests;
2. the store table and migration;
3. the orchestrator: intake from the meme channel and the reply command, with its locales;
4. the desktop queue: preview, approve, edit and reject;
5. documentation: the capability map and this plan's status.

## Tests

- Intake: tag plus attachment or link makes a proposal; a message with only one of them
  does not; messages outside the meme channel are ignored; a bot's own post is ignored.
- Trace: the meme poster and the tag author are kept apart, with the source link.
- Media: a video keeps its link, an image is kept as base64, and a non-file link is
  resolved through the post reader on approval; a reader refusal leaves it pending.
- Isolation: a pending proposal is never returned by chat retrieval, never cited and
  never delivered.
- Approval: an approved proposal becomes an example with `origin: group`, the proposer
  and the channel, and the proposal is gone; a rejected one is deleted.
- Precedence: owner over group over API for one meme, including an owner edit made after
  a group approval, and a new group proposal on an owner-tagged meme.
- Silence: no reply or channel message is produced by a proposal or its decision.
- Migration: an existing database opens and keeps its data.

## Risks and open questions

- **What is "anyone"?** All members who can write in the meme channel, or only listed
  people? Open channels invite spam, and since a rejection leaves no record, the same
  proposal can return. Per-proposer rate limits and a per-media duplicate check among
  pending proposals are likely needed.
- **Safety.** Proposed media should pass the same screening as any meme before it is
  previewed to the owner ([media admissibility](media-admissibility.md)).
- **Command name** and its Portuguese counterpart.
- **Edits and deletions.** What happens if the poster edits or deletes the message after
  proposing.
- **API tags.** Confirm in code where the source API's tags are read today, so the
  precedence rule replaces the current behaviour rather than duplicating it.
- **Other kinds of intake.** Which Knowledge intakes (links, PDFs) move to the proposal
  flow next, and whether the owner's own intakes skip it.
