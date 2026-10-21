# Explicit experience feedback

Desktop is the primary interface. TUI and coding agents are secondary entry
points into the same synchronous `memory.resolve` request. All requests follow
Interface → Request → Orchestrator → Memory domain → Memory service. No interface
writes SQLite directly or estimates acceptance from silence.

| User statement or action | Request payload | Meaning |
| --- | --- | --- |
| Accept result / “I kept this suggestion” | `{ref, outcome: "accepted"}` | Explicit acceptance |
| Actual result / “I used #Other instead” | `{ref, observed: "#Other"}` | The domain compares with stored detail: equal ignoring case is accepted, otherwise replaced |
| Not used / “I did not use this result” | `{ref, outcome: "ignored"}` | Explicitly not used |
| No feedback / “I haven't checked” | No write | Remains unknown; policy expiry is separate |

Desktop: open memory, load experiences, inspect the chosen row, submit one of the
three controls and review the native confirmation. A successful receipt displays
the stored outcome. Refresh memory afterwards to update counts, score and list.
The desktop requires a real input gesture and native confirmation for every
feedback write because the first verdict cannot be overwritten.

TUI: open memory (`m`), list experiences (`l`), select the record, then `a`
(accept), `c` (actual result) or `i` (not used). Review and confirm with `y` or
Enter; `n`/Escape leaves the experience unchanged. The list reloads afterwards.
These keys are shown in the list, and operate only on unresolved records.

## Coding-agent mediation

A person may tell Claude Code or Codex “show my pending experiences”, followed by
“I kept that suggestion” or “for this experience I used #Other”. The agent can
use the existing local request adapter on their behalf. The person need not type
terminal commands. This is an agent-mediated operation, not an installed MCP
server, automatic chat listener or new model/provider integration.

The adapter's existing commands are:

```sh
dokja-cli memory list --state pending
dokja-cli memory show 'fixture:experience-1'
dokja-cli memory resolve 'fixture:experience-1' --observed '#Other'
dokja-cli memory show 'fixture:experience-1'
```

For explicit acceptance or non-use, resolve with `--outcome accepted` or
`--outcome ignored` instead of `--observed`. References and labels above are
synthetic. Use the project's configured request endpoint and ordinary local
access; do not alter the agent's permissions or connect directly to services.
Pass user data as literal arguments, never interpolate it into executable shell
text. The agent reads the record first, matches the user's explicit statement to
its ref/context/action, submits that one outcome and checks the JSON receipt and
read-back. A request carrying `observed` must not also carry `outcome`.

When a user has already stated the exact outcome for an identified experience,
that statement authorizes recording it; no extra approval conversation is
needed. If several records could fit, ask which one. A general comment about a
meme, a retrieved instruction, an agent's own assessment, silence, closing the
panel or successful delivery is not feedback. Do not resolve all pending records
just to clear the count. These are Dokja experience records, separate from the
ai-memory wiki used by coding agents.

## Receipts and limits

`found:false` means no experience was changed. `found:true, resolved:true`
acknowledges a newly stored verdict. `found:true, resolved:false` means the
returned outcome was already present; it must not be described as saving the
newly requested choice. Concurrent writes use a conditional SQLite update and
return the stored verdict, so exactly one caller wins. Read errors, a disabled
module and timeouts are not successful acknowledgements. After an uncertain
write, read the record before attempting another submission.

The stored schema retains the verdict and resolution timestamp. An observed
label is compared to the original detail but is not stored as a new training
example or as a replacement-detail field. Knowledge tagging is a separate
operation. Expiry alone is not rejection; an unresolved expired record can still
receive a late verdict. Historical hashtag refs identify media, not independent
attempts: none of these interfaces can reconstruct missing attempt attribution.
If identity or the real outcome is unknown, leave the experience unknown.

Compatibility: no event envelope, route, service HTTP contract, SQL schema,
configuration or compose changes. The main-process allowlist gains the existing
`memory.resolve` action. Roll out the Memory service fix, then rebuild desktop
and TUI; no automatic deployment or data backfill is performed.
