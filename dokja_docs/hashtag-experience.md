# Hashtag experience rules belong to Memory

Step 09.4-g moves hashtag observation preparation out of the orchestrator and
into the [Memory domain](../dokja_domain/dokja_memory/hashtag.go), continuing the
accepted [ADR 0007](decisions/0007-faculties-not-features.md) migration. The
four-concept grouping in ADR 0010 remains proposed.

Memory owns the `hashtag.suggest` action name and stable experience reference:
`hashtag:` followed by the first 32 hexadecimal characters of the SHA-256 of
the trimmed media source reference. Recording, explicit feedback and task
evidence use the same function. Query strings remain significant; the source
is not otherwise canonicalized. Existing experiences need no backfill.

The domain prepares a value containing the observation, then builds fresh
prediction or record requests. Context is trimmed and limited to 4,000 Unicode
characters; matched evidence is trimmed and limited to 200. Positive match
scores are capped at one; nonpositive or absent scores remain absent. Blank
required note inputs retain the existing optional-note behavior. Generic
Memory validation still rejects invalid details, evidence and probabilities
before storage. Preparation neither writes an experience nor confirms an action.

The orchestrator retains the enabled-state check, background execution and
five-second workflow budget. Its compatibility suggestion path predicts before
recording; prediction failure still allows an unscored record. That path does
not establish a durable prediction-before-delivery guarantee.

The typed task path supplies its frozen pre-delivery prediction. Preparing its
record copies only the stored probabilities before background execution; later
edits cannot alter that payload. A missing prediction stays unscored and never
triggers another prediction during recording. Public handler prediction and
match types remain aliases to the domain types.

Explicit tagging prepares an observation addressed to the same experience.
Memory's existing resolution policy compares it with the stored detail,
case-insensitively, to produce accepted or replaced. Feedback does not create
a missing experience and does not edit Knowledge examples. The orchestrator
retains the existing successful-tag and successful-task-delivery call sites;
this extraction does not change delivery eligibility or feedback timing.

Compatibility review: no event envelope, route, action, provider contract,
response field, command, SQLite schema, environment variable, Compose service,
port or volume changes. Pool `mark-sent` receipts still belong to their existing
adapter; migrating those receipts, selection rules, Digest and conversation
responsibilities remains separate work. No live provider, account or owner
database is used for validation.
