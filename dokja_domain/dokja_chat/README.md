# Chat context and presentation policy

The Go package plans formula presentation for the orchestrator's chat handler.
The existing Python files remain the separate legacy chat adapter.

Context policy selects older turns for model summarization after inactivity,
near the raw-message limit, or at 32 KiB of raw history. It keeps up to eight
recent turns verbatim within 16 KiB, merges an existing summary, and validates the new summary
at no more than 4,096 UTF-8 bytes. Summary requests preserve speaker roles and
request goals, corrections, definitions, unresolved disagreements and attribution.
Summaries are lossy and untrusted, not system instructions or authorization.
The orchestrator owns inference calls and session storage; this policy does no I/O.

Generation can supply a fenced `math` block with single-line `latex:` and `text:`
fields. The readable text is always retained. Expressions over 80 bytes, multiple
fractions/roots, sums and integrals may receive a PNG; `image: true` requests an
image for a short expression too. Up to three expressions of at most 1,024 bytes
are selected per reply. The policy performs no rendering, network or file I/O.
Explicit `latex` source blocks remain untouched.

The orchestrator calls the rendering service and sends validated images through
the interface. Unsupported expressions or unavailable rendering preserve text.

Run `go test ./...` inside this module.
