# Design system and agent interface refactoring review

Architecture clarification (2026-10-29): the
[session/task architecture follow-up](session-task-architecture-review.md)
supersedes this review's UI-first dependency between `09.8` and `10.5`.
Backend lifecycle, dispatch and settlement contracts in `10.6` precede their
presentation; delegation does not depend on implementing the team interface.

Date: 2026-10-29. Scope: review and roadmap additions to the existing
[refactoring tracker](../steps.json), using the installed
`design-system-patterns` and `ui-ux-pro-max` skills, and the
[Cindy source study](cindy-architecture-research.md).
This document records planned work; it does not claim UI, agent runtime or
cognitive behavior has been implemented or validated.

Implementation update (2026-10-29): the owner requested starting the work.
[Batch 09.6-a](desktop-ux/design-tokens.md) adds compatible token layers and
migrates the page, state tones and base buttons. Foundation-only preparation
can proceed independently of cognitive acceptance; broader interface adoption
and the remaining `09.6` acceptance retain gate `08.3`. The original roadmap
review below remains a record of the planned sequence, not a claim that its
later steps have run.

## 1. Fit with the current refactoring

The active path remains `08 -> 08.2`: demonstrate understanding and application
across documents. The [cognitive realignment](cognitive-realignment.md) and its
`08.3` evaluation gate still precede broad adoption. Better presentation must
make evidence, uncertainty and outcomes inspectable; it cannot substitute for
that evaluation or establish cognitive boundaries by renaming workspaces.

The design-system skill applies to tokens, theme structure, reusable component
APIs and their migration. Cindy supplies separate architectural hypotheses about
sessions, controls and output presentation. The skill alone does not establish a
multi-agent architecture or justify a new execution engine.

No public envelope, action, route, schema, command, configuration, dependency or
deployment change is made in this planning batch. Each implementation step must
review its own compatibility surface before changing it.

The additional UI/UX review uses targeted local guidance for the existing
Electron/React interface, rather than generating a new visual direction.
Verified search matches were `Focus Not Obscured`, `Focusable Error Summary`,
`Error Placement`, `Virtualize long lists` and `Use keys properly`. Their
recommendations inform the acceptance criteria below; no search result is a
runtime audit or authorization to replace the approved identity. The list-size
heuristic is not a measured performance threshold for this application.

## 2. Source diagnosis and decisions to preserve

| Area | Evidence in the current checkout | Refactoring direction |
| --- | --- | --- |
| Tokens | `renderer/styles.css` defines palette, fonts, geometry and state colors together in `:root` and `[data-tone]` rules | Separate primitive, semantic and component layers through compatible aliases; retain current values initially |
| Theme | CSS declares `color-scheme: dark`; reduced-motion rules exist; a selectable light theme is not established in the inspected renderer | Keep the approved dark appearance as the baseline; separate theme values from components without silently adding a new palette or settings contract |
| Components | `WorkspacePanel`, `Drawer`, `useViewTabs`, shared problem presentation and context menus already exist | Audit and extend these primitives before introducing replacements or a new component framework |
| Composition | `WorkspacePanel` already accepts views, actions, dock and body slots; `Drawer` renders through a portal and manages focus | Preserve composition and overlay placement while extracting reusable controls |
| Shared state | `StatusProvider` owns the shared status read; conversation hook owns local transcript, draft and preview state | Keep status ownership; move durable conversation truth through the existing archive API, not a new UI store |
| Conversation archive | History plan records H1/H2 implemented and H3-H5 pending; current desktop `useConversation` still stores turns in React state | Finish `09.5`; reuse its identities, pagination and retry semantics in task presentation |
| Inference | ADR 0011 assigns bounded proposals to providers and application control to Dokja | Add capability/progress projections without granting a provider workflow control |
| Design records | `ui-system.md` records owner decisions, but its later proposal tables and mockup retain rejected alternatives | Label historical proposals clearly; derive the component catalog from the approved implementation baseline |

Source paths above are under `dokja_interfaces/desktop/src/` unless otherwise
specified. Additional anchors: [conversation hook](../dokja_interfaces/desktop/src/renderer/cards/assistant_conversation.tsx),
[workspace panel](../dokja_interfaces/desktop/src/renderer/cards/workspace_panel.tsx),
[drawer](../dokja_interfaces/desktop/src/renderer/cards/drawer.tsx),
[status context](../dokja_interfaces/desktop/src/renderer/status_context.tsx),
[conversation history](conversation-history-plan.md), and
[inference responsibility decision](decisions/0011-inference-provider-boundaries.md).

Preserve the owner-approved cyberpunk identity: cyan buttons, glow, colored-square
states, bundled B612 Mono/Bodoni Moda typography and chamfered panels. Keep the
accepted voice-line, sidebar wording, panel container and progress-tone choices.
The rejected neutral-button, reduced-glow and alternative state-shape proposals
do not become requirements because a generic skill recommends consistency.
Accessibility work should provide readable state labels and keyboard behavior
within those choices.

## 3. Review of the existing refactorings

| Existing step | Review result | Change to the plan |
| --- | --- | --- |
| `08.2` / `08.3` | Retain application behavior and frozen evaluation as the active work | Add this review as a reference; no new provider run, altered fixture or completion claim |
| `09.3` consolidation | Retain domain validation and evidence before knowledge promotion | New presentation exposes support, conflict and applicability instead of treating generated summaries as accepted knowledge |
| `09.4` responsibility migration | Retain completed batches and proposed grouping status | Component boundaries follow reusable interactions; they do not create a new four-group or nine-group navigation mandate |
| `09.5` conversation history | Retain and extend acceptance criteria | Product conversation/send identities remain separate from provider sessions and bounded inference context; no parallel history implementation |
| `10.3` inference providers | Retain bounded inference boundary | Expose capability limits, progress provenance and terminal/retrying distinctions; unsupported controls must not be simulated |
| `11.4` provider settings | Retain registered provider and binding work | Reuse component primitives; distinguish configured availability, supported capability and observed execution |
| Deployment/storage work | Retain existing evidence and rollout gates | UI preferences do not introduce a new database, remote relay or mobile app; deployment changes need their own demonstrated requirement |

## 4. Added steps and deliverables

### 09.6 — Establish compatible token and theme foundations

After `08.3`, inventory actual CSS values and approved identity choices. Define
primitive tokens, purpose-based semantic aliases and component tokens only where
they remove real duplication. For example, a palette cyan can supply separate
selected-state and action roles without changing its initial value.

Add spacing, typography, focus and motion tokens from existing measured usage.
Retain old names as aliases while migrating consumers in batches. Document the
mapping and removal conditions; do not require Figma, Style Dictionary or another
pipeline without a demonstrated consumer.

Keep dark mode as the initial supported theme. Verify reduced-motion behavior
and readable focus/state presentation, including forced-color environments.
Additional themes require an explicit visual specification before implementation;
the architectural separation itself does not approve a light palette.

Acceptance evidence: resolved-token checks for missing/circular references,
representative before/after screenshots, readable states and keyboard focus,
reduced-motion coverage and the relevant desktop checks. A CSS rename alone
does not establish accessibility.

### 09.7 — Consolidate accessible UI primitives incrementally

Depend on `09.6`. Inventory existing panel, drawer, menu, tab, form, action,
status and feedback components. Define small explicit variant/size APIs where
needed; retain native element semantics and the existing slot composition.
Avoid arbitrary polymorphism, generic prop bags or dependency changes without
an actual composition need.

Build an implementation-backed state catalog: loading, empty, ready, stale,
unavailable, pending, error and unsupported where meaningful. State wording and
interaction contracts are reusable across surfaces, while CLI/TUI rendering
remains platform-specific.

Migrate a representative vertical first, then remaining consumers in cohesive
batches. Verify focus entry/return, modal keyboard containment, visible labels,
tab navigation, disabled/pending actions, long content and container-width
behavior. Preserve the portal placement that keeps overlays outside panel size
containers. Keep a mapping and rollback path for each migrated consumer.

Include a focusable error summary when a multi-field submit fails, linked to
inline field errors; preserve entered values. Docks, sticky headers and drawers
must not hide the focused action. Relevant errors and completion changes need
accessible announcements without reporting every streaming token. Verify zoom,
text growth and keyboard use at the supported narrow desktop size, rather than
claiming native mobile support from a responsive layout.

### 09.8 — Expose conversation and task state through shared contracts

Depend on `09.5`, `09.7` and backend contract `10.6`, using domain-defined lifecycle policy and existing
orchestrator task outcomes. Keep conversation, send, workflow execution and
native provider identities distinct. Define a bounded versioned projection for
supported progress and controls; review additive ingress/action compatibility
before exposing new operations.

Distinguish accepted/queued input, running work, provisional preview, final
answer, awaiting input, failure, interruption and unknown state only when the
backend can establish them. Keep provider availability, capability support and
execution state separate. Cancellation and same-turn steering are separate
capabilities; do not render working controls for absent backend operations.

Exercise the same state fixtures in desktop and CLI/TUI through orchestration.
Late replies must remain attached to their original task/send identity after
navigation or restart. No remote-device control or mobile client is implied.

While a reply streams, preserve the draft, selection and the user's position
when reading earlier messages; auto-follow only when appropriate. Use stable
message/task identities for changing lists. Measure long-history rendering and
input responsiveness before choosing pagination/windowing changes; virtualization
must retain keyboard access, accessible ordering and focus recovery.

Acceptance evidence: existing archive restart/idempotency checks plus projection
fixtures for old/missing capabilities, stale results, retries, final versus
preview output and inaccessible actions refused before effects.

### 09.9 — Evaluate bounded declarative answers

Depend on `09.8`. Prototype an opt-in versioned answer representation for cited
text, small tables/status summaries and a narrowly defined action/form subset.
Keep a plain-text equivalent for CLI/TUI and messaging. Preserve ordinary text
answers and existing media behavior.

Treat generated nodes and action arguments as untrusted data. Bound document
bytes, node count/depth and field lengths; reject unknown versions and unsafe
content explicitly. Use approved native components and semantic tokens rather
than arbitrary HTML, scripts, CSS or general IPC calls. An offered control does
not itself authorize an action: submissions return through validated ingress
and domain policy, with stable send identities preventing duplicate execution.

Acceptance evidence: incremental/incomplete content, malformed nodes, unknown
versions, unsupported controls, duplicate submission, navigation and plain-text
fallback fixtures. Demonstrate a concrete operator benefit before adopting it
beyond the prototype; no cognitive success claim follows from rich rendering.

### 10.5 — Evaluate domain-owned delegated tasks before adding agent teams

Depend on `10.3` and `10.6`; UI step `09.8` is not a prerequisite. Decide whether a real workload benefits from
delegation compared with the existing bounded single-task workflow. Define a
parent/child task contract, scoped evidence, permitted operations, acceptance
rules, cumulative budgets, reporting and cancellation before runtime integration.
Use deterministic workers first; retain an explicit no-adoption outcome.

The experiment must distinguish task execution, report delivery and accepted
completion. Test duplicate/late reports, partial failure, cancellation, restart
and unauthorized operations. Compare correctness, cost and latency against the
single-task baseline on frozen cases.

Do not adopt Cindy's lead-agent authority wholesale. Domain policy validates
delegation and results, the orchestrator sequences approved work and services
execute bounded capabilities. A provider cannot spawn arbitrary agents, write
memory or perform external effects through a new control path. Remote workers,
provider-native teams and device relays remain outside this experiment unless
a separately reviewed requirement justifies them.

## 5. Ordering and verification

1. Continue the remaining `08.2` semantic/application work and `08.3` controls.
2. Complete validated consolidation and existing history work; token/component
   foundations can proceed after the same evaluation gate without changing it.
3. Define backend lifecycle/dispatch/settlement in `10.6` and integrate inference
   through `10.3`. Task-state presentation consumes those contracts; delegation
   is evaluated independently of implementing the UI. Declarative answers remain
   a separate interface experiment.
4. Run checks for the modules actually changed. Desktop implementation uses its
   existing `pnpm test`, `pnpm typecheck`, `pnpm build` and relevant
   `pnpm test:e2e` scenarios; Go changes use `go test ./...` in each changed
   module. Record blocked native/live checks separately from passing fixtures.

For this planning batch, validation covers JSON shape, unique IDs, dependencies,
resume gates, acyclic ordering, preservation of existing evidence/statuses and
local documentation links. No application suites, visual acceptance, live agents
or cross-device behavior were exercised. New implementation steps remain pending.
