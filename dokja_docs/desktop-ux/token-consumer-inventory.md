# Desktop token consumer inventory

Source review: 2026-10-29, after foundation `09.6-a` and focus/motion preparation
`09.6-c`. This inventory covers renderer CSS responsibilities, not every runtime
state or a completed accessibility audit. It supplements the
[token migration contract](design-tokens.md).

## Migrated consumers

| Consumer | Roles and preserved defaults | Verification scope |
| --- | --- | --- |
| Page/body | Page surface, body text and bundled font; 15 px / 24 px at default zoom | Native default-style assertions and Talk/System samples |
| Shared state tones | Separate working, problem, pending, off and progress roles | Existing tone/UI fixtures; native working-state independence |
| Base buttons | Component text/border/hover roles, primary-ink focus with 2 px width and 3 px offset | Native default and independent-override fixtures |
| Inner view tabs | Primary-ink focus via `--tab-focus-ring`, preserving 3 px offset | Source migration; full native tab/focus traversal remains pending |
| Search, settings, add forms and model input | Cyan accent focus; 2 px width and 2 px offset | Source migration; native composer probe exercises the shared accent role |
| Composer, message input, classification and workspace selects | Same accent role and tight offset | Native composer default/override and forced-color samples; other consumers require their own interaction checks |
| Source/advanced summaries, reading preview, media picker and menu rows | Accent focus, preserving each existing 2 px/3 px offset | Source migration; keyboard/focus-obscuring checks remain pending |
| Service rows | Reveal 500 ms with 70 ms staggering | Native timing and reduced-motion fixtures |
| Service-progress and workspace-status markers | 1000 ms blink | Native pseudo-element/reduced-motion fixtures |
| Wordmark | 450 ms stepped transition | Native timing and reduced-motion fixtures |
| Drawer | 160 ms entry, only with no reduced-motion preference | Native timing, independent overlay-duration override and reduced-motion fixtures |
| Talk conversation and composer | `--conversation-column`, `--turn-*`, `--composer-field-*`, `--button-compact-*`: one centred column, the reply's rule and the owner's plate as roles, a compact button size; no palette meaning changed (the reply's rule is the panel edge, not a neon) | Design-token graph test, a test that every `var()` in `styles.css` is declared, Talk UI tests, browser screenshots at 800/1024/1536 CSS px, forced-colors and reduced-motion emulation; no native Electron capture |

The source migration replaces 13 solid cyan outline declarations and the inner
view-tab ink outline. It retains the base-button mapping from `09.6-a`.
The selector groups include multiple element types; declaration counts are not
the number of independently verified interaction paths.

## Remaining roles to classify before migration

| Existing uses | Responsibility to separate | Limit for the next batch |
| --- | --- | --- |
| `--cyan` in selected sidebar/view entries, switches and selected source/channel rows | Selection, enabled state and action roles | Inspect each selector and its domain state; do not replace all cyan uses with the action token |
| `--cyan` in row-menu hover/focus border and background | Menu interaction feedback | Preserve the visible border/wash alternative when outline is suppressed; test keyboard and forced colors before changing it |
| Cyan/magenta wordmark shadows and status glow | Approved identity/decorative effects | Preserve owner-approved glow; do not apply rejected mockup alternatives |
| Cyan conversation borders, reading/drop highlights and media selection | Role-specific emphasis and affordances | Distinguish user selection, drag affordance and decorative emphasis |
| `--ink`, `--ink-dim`, `--panel`, `--static`, `--edge`, `--rule` throughout feature panels | Primary/secondary text, surfaces, strong/subtle borders | Existing aliases keep behavior; migrate with the owning component rather than through a global rename |
| `--magenta`, `--magenta-text`, `--amber`, `--off` in notices and status details | Problem, caution/pending and inactive presentation | Keep fill and readable text distinct; preserve visible words and accessible names |
| Raw white, translucent shadows, scrims, scanlines and noise | Selection contrast, overlays and texture | Assign roles only after checking contrast/stacking; no new theme or palette is implied |
| Repeated feature font sizes, spacing and geometry | Type/spacing/component measurements | Inventory actual use before enforcing a uniform scale; keep font metrics and panel shape stable |

Compatibility aliases remain until all their consumers are deliberately
migrated. Root-level role overrides are supported as documented; arbitrary
subtree theme inheritance is not introduced by this batch.

## Verification boundaries

The focus/motion suite runs Chromium inside Electron against a synthetic local
orchestrator. Media emulation verifies CSS selection for reduced motion and
forced colors. It does not validate every operating-system contrast palette,
assistive technology, control's accessible name or keyboard traversal path.

`tests/e2e/acceptance.e2e.ts` now runs every workspace in Electron against a synthetic
orchestrator: no sideways overflow and a visible dock at 1536x860, 1100x800 and 800x700;
no overflow with text at 200%; a visible focus ring on what real Tab navigation reaches
(14 stops per workspace, not every control), with the stops required to land inside the
open workspace when it has controls; and no animation running under reduced motion with
forced colors on. Coverage follows the synthetic state: Realtime has no controls there,
Messages has one and the Library is in its failed state, so those screens were not
exercised full of content. 800x700 does not reach the layout for narrow panels. It found one real defect: a note in a System row widened
its column past the panel at 1100x800, fixed by giving the note the whole row. The
suite locates workspaces by their stable ids instead of their position.

Still not validated: screenshots from the native window, which time out in the
headless compositor, so visual review rests on browser captures; assistive
technology; native high-contrast palettes; menu interactions and focus around open
drawers per workspace; windows narrower than 40rem, where the docked composer falls
below the window. Gate `08.3` still precedes broader interface adoption.
