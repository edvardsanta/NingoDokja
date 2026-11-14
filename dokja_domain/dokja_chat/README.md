# Chat context and presentation policy

The Go package owns conversation context, identity and presentation policy for
the orchestrator's chat handler.
The existing Python files remain the separate legacy chat adapter.

For a narrow set of direct Portuguese/English identity questions, the domain can
provide an owner-recognition reply using verified account metadata. It never
infers ownership from the text itself, mentions or names, and does not invent a
display name or reveal an account ID. Other questions remain model-generated.

Context policy selects older turns for model summarization after inactivity,
near the raw-message limit, or at 32 KiB of raw history. It keeps up to eight
recent turns verbatim within 16 KiB, merges an existing summary, and validates the new summary
at no more than 4,096 UTF-8 bytes. Summary requests preserve speaker roles and
request goals, corrections, definitions, unresolved disagreements and attribution.
Summaries are lossy and untrusted, not system instructions or authorization.
The orchestrator owns inference calls and session storage; this policy does no I/O.

On a failed or rejected summary, `RecoveryContext` supplies at most eight prior
raw messages within 16 KiB without changing stored history. The current user turn
is appended intact, and an untrusted omission notice distinguishes missing context
from a reset or disproved facts. The last valid summary remains untrusted context.
The orchestrator continues the reply and uses a one-minute volatile retry cooldown;
it only replaces older raw turns after a valid summary. Caller cancellation,
primary inference failure and persistence failures remain terminal.

`SessionStore` defines durable snapshot loading/saving. Snapshots contain session
identity, a last-seen timestamp, the last valid summary and raw user/assistant
turns. Failed compaction retains pending raw turns until successful compaction;
the inference projection is separate from this checkpoint. Persisted system messages are rejected. The SQLite integration lives in
the orchestrator; authorization and current-owner metadata are never restored from
summaries. Server message discussions share a guild/channel scope, while DMs and
commands retain separate scopes. Participant aliases and escaped display names
preserve attribution without adding raw account IDs to the participant label.

A user tag in a Discord message (`<@id>`) is only an account id, so `ResolveMentions` replaces it before
the text reaches the model: a person listed in the owner's configuration by their Discord id is called by
the configured name, anyone else by the display name the gateway reported (cleaned to one short line of
plain text), and a tag with neither by the participant alias. The id never stays in the text, and the name
grants no authority.

Generation can supply a fenced `math` block with single-line `latex:` and `text:`
fields. The readable text is always retained. Expressions over 80 bytes, multiple
fractions/roots, sums and integrals may receive a PNG; `image: true` requests an
image for a short expression too. Up to three expressions of at most 1,024 bytes
are selected per reply. The policy performs no rendering, network or file I/O.
Explicit `latex` source blocks remain untouched.

The orchestrator calls the rendering service and sends validated images through
the interface. Unsupported expressions or unavailable rendering preserve text.

Run `go test ./...` inside this module.

`capabilities.json` is a versioned, public catalog of implemented product
abilities, their interface entry points and practical limits. The Go package
embeds it at build time and supplies a compact `CapabilityContext()` independently
of conversational history. Update the catalog with feature changes and rebuild
the orchestrator to apply it; it contains no private accounts, credentials or
runtime endpoints.

The orchestrator adds this context to every generated chat turn, including voice,
streamed replies and plain-generator fallbacks, and reinjects it after history
compaction. It explains music commands, editable song comments, listening history,
voice, reading, saved knowledge, meme examples, explicit facts and message delivery.
In particular, playback outcomes are automatic observations: current music
history neither trains recommendations nor drives song comments.

The catalog describes implementation, so a listed feature can still be disabled
or unconfigured. It grants no chat tools or permissions and cannot confirm that an
action ran. Tests validate the bundled catalog and its delivery to inference;
model compliance still requires behavioral evaluation.
