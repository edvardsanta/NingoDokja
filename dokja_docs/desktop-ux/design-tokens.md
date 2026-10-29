# Desktop design tokens: first migration batch

Implemented: 2026-10-29, step `09.6-a`. This is a compatible foundation batch;
the broader token/accessibility/visual acceptance in `09.6` remains pending.

## Ownership and migration

[tokens.css](../../dokja_interfaces/desktop/src/renderer/tokens.css) defines the
approved dark palette in three layers:

| Layer | Examples | Purpose |
| --- | --- | --- |
| Primitive | `--color-cyan-400`, `--color-slate-100`, `--space-4` | Existing concrete palette and measurements |
| Semantic | `--action-primary`, `--status-ok`, `--text-primary`, `--surface-page` | Distinct responsibilities, even when defaults match |
| Component | `--button-text`, `--button-border`, `--button-focus-ring` | Base-button presentation without changing unrelated state roles |

The stylesheet is imported first by the existing renderer CSS. The initial
batch migrates the page background/body typography, shared state-tone mappings
and base-button colors/focus. Decorative textures, glow and feature-specific
rules retain their existing behavior. No theme selector or new palette is added.

Defaults are declared on `:root`. A root-level semantic override updates its
dependent component defaults. For a local component override, set the component
role itself; inherited CSS custom-property aliases resolve on the element that
declares them, so overriding a semantic role only on a descendant is not a
promise to recompute all inherited aliases there. Subtree themes are outside
this first batch.

New migrated consumers should choose a role by responsibility. A working-state
indicator uses `--status-ok`, not `--action-primary`, even though both start cyan.
Existing consumers retain these aliases until each is deliberately migrated:

| Existing name | Mapping |
| --- | --- |
| `--static`, `--panel` | `--surface-page`, `--surface-panel` |
| `--edge`, `--rule` | `--border-default`, `--border-subtle` |
| `--ink`, `--ink-dim` | `--text-primary`, `--text-secondary` |
| `--cyan` | Original cyan primitive; ambiguous old uses remain for later migration |
| `--magenta`, `--magenta-text` | Problem fill and text roles |
| `--amber`, `--off` | Pending and off-state roles |
| `--mono`, `--serif` | Body and display font roles |
| `--cut`, `--chamfer` | Panel geometry roles |

Do not remove an alias before its CSS consumers and relevant tests have migrated.
Changes to token meaning or removal are compatibility changes. The initial
spacing values describe existing usage; this batch does not force every
feature-specific measurement onto the scale.

## Verification and limits

- `pnpm test`: 258 Node tests and 322 UI tests passed, including the new token
  dependency-graph check for duplicate definitions, missing references and cycles.
- `pnpm typecheck` and `pnpm build` passed. The build uses the existing toolchain
  and unchanged lockfile; generated output is not part of the change.
- `node --import tsx --test tests/e2e/design_tokens.e2e.ts`: four native Electron
  tests passed using an isolated display and synthetic local orchestrator. They
  check existing palette/type defaults, independent action/component overrides
  and resolved legacy aliases in Chromium.
- Independent before/after captures of 37 visible nodes in Talk and System
  showed identical computed color, typography, border, spacing, dimensions,
  clipping and text-shadow values. This is a sampled computed-style comparison,
  not full pixel or accessibility acceptance.
- Screenshot capture timed out in the isolated compositor, including its alternate
  capture path. Screenshots and manual visual acceptance are not reported as
  passing. Forced-color, zoom/text growth, all-workspace visual coverage and
  additional focus/motion migration remain pending.

All runtime checks used synthetic fixtures. No live provider, account, owner
conversation, database, platform message or application stack was exercised.
The full native e2e suite was not run for this scoped CSS batch; the existing
smoke test assumes five workspaces while the current registry has six.

## Rollback

Revert this batch's token stylesheet, its import/consumer changes and tests as
one unit. No persisted settings, schema, dependency, public action or deployment
change requires a migration. Preserve subsequent consumer work if rolling back
after additional migration batches.
