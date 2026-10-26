# Ningo desktop: visual system and mockups

Date: 2026-10-23. Author: UI designer agent. Input: `ux-proposal.md` (owner decisions of 2026-10-23
are binding), the current renderer styles
(`dokja_interfaces/desktop/src/renderer/styles.css`), and the identity in `README.md` and
`dokja_docs/desktop-app-plan.md`. Design only: nothing outside this folder was changed.

- Mockup: `mockup/index.html` (one file, inline CSS, minimal JS, pt). Open it from disk. Routes by
  hash: `#conversa`, `#conversa-vazia`, `#conversa-offline`, `#biblioteca`, `#biblioteca-leituras`,
  `#biblioteca-adicionar`, `#biblioteca-documento`, `#biblioteca-vazia`, `#leituras-vazia`,
  `#caderno`, `#caderno-detalhe`, `#mensagens`, `#mensagens-meme`, `#mensagens-whatsapp`,
  `#mensagens-silencio`, `#sistema`, `#sistema-detalhe`, `#sistema-offline`,
  `#sistema-configuracoes`, `#sistema-provedores`, and `#componentes` (the component catalog with
  every state of every control, only in the mockup). Ctrl+1..5 switch workspace.
- Screenshots: `screenshots/ui-<view>-<1280|1024>.webp`, listed in section 12. The "before" images are
  `screenshots/01-...17-*.webp`.
- Fonts: the mockup names `"B612 Mono"` and `"Bodoni Moda"` and loads nothing. The screenshots were
  rendered from a throwaway copy with the bundled font files inlined, so they show the real faces; opened
  on a machine without those fonts the mockup falls back to a monospace and a serif.

## 0. Proposals that need the owner

The brief says a change of palette meaning, fonts or the wordmark is the owner's call. Nothing below
changes a colour's meaning, a font or the wordmark. These do change how the identity is *applied*, and
the mockup is built with them, so each one is listed to be accepted or reverted.

**Owner's decision (2026-10-23).** Accepted: P2 (the voice line in Bodoni italic), P4 and P6 (a quiet
legend; the sidebar says a word only when something is not well) and P7 (plain copy says "Biblioteca",
which follows from the owner's names). Not taken: P1, P3 and P5. P8 and P9 were accepted on 2026-10-26: the workspace panel
is a size container (`ws`), and a module starting or stopping takes the `progress` tone. The drawer,
the row menu and the media viewer are drawn on the page itself, outside the panel, because a size
container would otherwise hold their fixed position. The owner likes the
cyberpunk look as it is, so buttons stay cyan, the glow stays, and a state keeps its coloured square
without a shape of its own. The mockup still shows the rejected proposals; the app does not.

| # | Proposal | Why | What reverting costs |
|---|---|---|---|
| P1 | **Buttons are neutral.** Secondary = ink text on an `--edge` border, primary = `--ink` fill with panel-coloured text. Cyan leaves buttons. | Today every button is cyan outline and a pressed tab or mode paints like a switch that is on. The identity rule "neon only means a state" is broken by the buttons themselves. | Cyan buttons make "primary" impossible to tell from "on". |
| P2 | **The voice line is Bodoni Moda 700 italic, 1.25 rem, `--ink`**, one per view, always followed by a plain sentence (section 6). The face is already bundled. Ningo's long replies stay in B612. | The proposal asks for it (its question 5 is not in the owner's 2026-10-23 list, so it is still open). Kept modest so it never competes with the wordmark. | The voice would have to be told apart by copy alone. |
| P3 | **Glow is removed everywhere except the wordmark ghosts.** No text-shadow on the open sidebar entry, no box-shadow on dots or the service strip. | "Glow used as decoration" is on the not-wanted list. The state is carried by colour plus shape plus word. | Restore two lines of CSS. |
| P4 | **The sidebar is a chamfered panel like the work area** (replan: "the same chamfered panels"), and the legend is a quiet breadcrumb (`--ink-dim`, current segment `--ink`), not cyan. | The open workspace is already lit cyan in the sidebar; a cyan legend repeats the same state. | Legend back to cyan. |
| P5 | **State has a shape as well as a colour**: square = working, triangle = problem, hollow diamond = unchecked or waiting, dash = off. | Colour alone is not enough for colour-blind owners (WCAG 1.4.1), and the proposal asked for it. | Dots stay colour-only squares. |
| P6 | **The sidebar says a word only when something is not well** ("aguardando vínculo", "precisam de atenção"). When well, it is a shape. The full phrase is always in the accessible name. | Restraint: the exception is spelled out, the rule is silent. A word on every entry did not fit the 13.5 rem column. | Add a word to each entry. |
| P7 | **Plain copy that names the workspace says "Biblioteca"**, not "conhecimento" (the `ux-proposal.md` glossary says never "biblioteca" in plain copy; the owner's rename supersedes it). The search placeholder is "Buscar na Biblioteca…", the drawer "Adicionar à Biblioteca". Section 5 of the proposal needs this one-line update. | Owner decision: workspaces are Conversa / Biblioteca / Caderno / Mensagens / Sistema. | n/a |
| P8 | **The aside is inline or an overlay by the width of the panel (62 rem), not of the window.** A panel is 62 rem wide when the window is at least about 1256 px, so at 1024 the aside is an overlay. | The proposal says "under about 1000 px", but with a 13.5 rem sidebar a 1024 window leaves a 760 px panel. A 22 rem aside inline would leave 408 px for the list. Needs a container query (supported by the Chromium in Electron). | Use a window query at 1256 px. |
| P9 | **New tone `progress`** uses `--ink`, not a neon, for "starting" (blinking square). | In-progress is not up, not a problem, not unchecked. | Use cyan and lose the meaning. |

## 1. What changed from the current look

| Area | Now (screenshots 01-17) | Proposed (mockup) |
|---|---|---|
| Frame | A card per section, three in System, legend repeats the sidebar word, footer "Atualizar" in each | One chamfered panel per workspace that fills the window, legend is a breadcrumb (`biblioteca › documentos`), one refresh icon, only the body scrolls |
| Sidebar | Strip with a right border, wordmark above a duplicate "Ningo" entry, dot only | Panel, wordmark is a link to Conversa, five entries, dot with a shape, a word only when not well, Sistema at the foot with `▲ 3` |
| Buttons | All cyan outline; `aria-pressed` fills cyan | Neutral; one ink-filled primary per view; cyan only for selected and on |
| Switch | A button painted cyan, label "ligado" | Track and thumb with the word ("Ligado"), cyan only when on, amber and dashed when not checked |
| Tabs and modes | Pressed buttons, underline tabs, sidebar tabs: three idioms | Tabs = cyan underline; segmented = cyan outline, no fill; sidebar = cyan bar on the left; switch = track |
| Lists | Block of text plus two buttons per row | Table or row with one open action, a `⋯` menu, relative times, state as shape plus word |
| Voice | None except the wordmark | Bodoni italic line, one per view, with a plain next step |
| Glow | On the open sidebar entry, dots, service strip | Only the wordmark ghosts |
| Chat | A form in a card, composer moves down | Transcript scrolls anchored at the latest turn, composer docked |

## 2. Principles applied

1. **Neon only means a state.** Cyan = on, selected, open. Magenta = a problem. Amber = not checked or
   waiting. Nothing else is neon. Focus is ink, not a colour of state.
2. **One meaning, one component.** A switch, a button, a segmented control and a tab never look alike
   (section 5).
3. **Quiet when well.** A healthy screen has no neon beyond one selected entry and small shapes. Problems
   get words and a fix beside them.
4. **The name is the one memorable thing.** The wordmark ghosts are the only glow. The voice line is
   the one place with theatre and it is small.
5. **Structure carries information.** Chamfered panels, hairlines, a left bar for a state.

## 3. Tokens

### 3.1 Kept (already in `styles.css`)

| Token | Value | Role |
|---|---|---|
| `--static` | `#101b25` | The page, and input wells |
| `--panel` | `#0a1119` | Panel and sidebar surface (opaque) |
| `--edge` | `#456a80` | Panel edge, control borders, rules at full strength |
| `--ink` | `#d6e6f3` | Text, focus ring, primary button fill |
| `--ink-dim` | `#8ea3b5` | Secondary text, placeholders |
| `--cyan` | `#3be8f5` | On, selected, open |
| `--magenta` / `--magenta-text` | `#ff3d8b` / `#ff6aa5` | Problem (fills, bars / text) |
| `--amber` | `#ffb22e` | Not checked, waiting |
| `--off` | `#62768a` | Off: shapes only, never text |
| `--cut`, `--chamfer` | 12 px, polygon | Two cut corners (top right, bottom left) |
| `--mono`, `--serif` | B612 Mono, Bodoni Moda | Everything / wordmark and voice |
| `--scanlines`, `--noise` | gradient, SVG | The dead channel on the page |
| `[data-tone]` blocks | `ok`, `problem`, `unchecked`, `off`/`other` | Set `--tone` and `--tone-text` |

### 3.2 New (flagged)

| Token | Value | Why |
|---|---|---|
| `--rule` | `color-mix(--edge 45%, transparent)` | Hairline between header, toolbar, body and dock; same hue, quieter |
| `--wash` | `color-mix(--ink 6%, transparent)` | Hover on rows and entries (no new hue) |
| `--wash-strong` | `color-mix(--ink 12%, transparent)` | Active press, icon button hover, menu hover |
| `--focus` | `var(--ink)` | One focus ring for everything: 2 px solid, 2 px offset (inset on rows and the sidebar) |
| `--fs-xs` `--fs-sm` `--fs-base` `--fs-md` `--fs-lg` `--fs-voice` | 0.75 / 0.8125 / 0.9375 / 1.0625 / 1.375 / 1.25 rem | Type scale (section 3.3). `--fs-sm` and `--fs-base` match the sizes `styles.css` already uses ad hoc |
| `--measure` | `68ch` | Reading width for transcripts, documents, summaries |
| `--s-1`..`--s-7` | 0.25, 0.5, 0.75, 1, 1.5, 2, 3 rem | Spacing, a 4 px grid (section 3.4) |
| `--row-h` | `2.25rem` | Height of a table row and a sidebar entry |
| `[data-tone="progress"]` | `--tone: --ink` | In-progress (P9) |

No new colour hue is introduced. Everything new is a derived mix of the kept inks, a size, or a space.

### 3.3 Type scale

B612 Mono 400 and 700 are bundled; Bodoni Moda only as 700 italic. Spec nothing else: any other weight
or style is synthesised by the browser.

| Step | Size / line | Used for |
|---|---|---|
| xs | 12 / 1.4 | Timestamps, counts, helper text, table headers, sidebar word, chips |
| sm | 13 / 1.5 | Table cells, buttons, fields, legend, notices |
| base | 15 / 1.6 | Reading text, transcript, tabs, sidebar entries |
| md | 17 / 1.35, 700 | Item and section titles (reading title, channel name) |
| lg | 22 / 1.3, 700 | Document title only |
| voice | 20 / 1.3, Bodoni 700 italic | The voice line only |
| mark | 48 / 1, Bodoni 700 italic | The wordmark only |

Reading measure is 68 ch for running text. Numbers in tables use the mono face, so columns align
without tabular figures.

### 3.4 Spacing, shape, motion

- **Panel:** page padding 1 rem, gap 1 rem between sidebar and work area. Header 3.25 rem. Body padding
  1.5 rem (flush for tables). Dock padding 1 rem 1.5 rem. Aside 22 rem.
- **Rhythm:** 4 px base; related items 4-8 px, groups 16-24 px, sections 24-32 px.
- **Rows:** 36 px (`--row-h`). Two-line rows (experiences) are about 57 px. Hit area of an icon button is
  32 px (28 px in `btn-sm`); the minimum target is 24 px (WCAG 2.5.8).
- **Shape:** rectangles for everything the owner can focus (a clip-path would cut the focus ring); the
  chamfer is for panels only. No radius anywhere. The switch track is a rectangle.
- **Elevation:** only menus, popovers and the overlay aside have a shadow, and only to separate them from
  what is under them.
- **Motion:** one terminal print (`steps(24)`, 0.5-0.8 s) on first mount of rows and the voice line; one
  blinking cursor for loading and busy. `prefers-reduced-motion` stops both, content stays fully visible
  (checked: zero running animations under reduce).

## 4. State vocabulary

One state is always three things: a colour, a shape and a word. The word is always in the accessible
name; in the sidebar it is spoken aloud by the label and drawn only when the state is not "well".

| Tone | Colour | Shape (CSS) | Word (pt) | Used by |
|---|---|---|---|---|
| ok | `--cyan` | filled square, 8 px | Funcionando, Ligado, Encontrado | services, switches, indexing complete |
| problem | `--magenta` (text `--magenta-text`) | filled triangle | Sem resposta, Falhou, Com falha | services, sources, errors |
| unchecked | `--amber` | hollow diamond | Sem verificação, Aguardando vínculo, Aguardando veredito | status not read, WhatsApp not linked, experience waiting, passages not indexed |
| off | `--off` (text `--ink-dim`) | dash | Desligado, Parado, Desativada, Pausado | stopped or paused |
| progress | `--ink` | blinking square | Iniciando…, Aplicando… | in flight |
| unknown | `--ink-dim` | none | an em dash | no data |

Text colours: only `--ink`, `--ink-dim`, `--cyan`, `--magenta-text` and `--amber` are used for text. `--off`
(4.04:1 on the panel) and `--magenta` (4.44:1 on the lightest page pixel) fail 4.5:1 as text and are
used for shapes and fills only.

## 5. The four control looks

Written first because everything else depends on them.

| Control | Means | Look | Never |
|---|---|---|---|
| **Button** | A verb. Primary: `--ink` fill, panel text, bold, one per view. Secondary: 1 px `--edge` border, ink text. Quiet: no border, dim text, underline on hover. Icon: 32 px, no border. | Neutral. Hover brightens the border to ink and adds `--wash`. | Cyan. |
| **Switch** | A persistent state that applies at once. | 36 x 18 px track with a 12 px thumb **and the word beside it**. On: cyan track outline, cyan 18 % tint, thumb at the right, word cyan "Ligado". Off: `--off` track, thumb left, word dim "Desligado". Not checked: amber dashed track, hollow thumb, "Sem verificação". | Looking like a button. |
| **Segmented** | A mode or a filter (Nota / Endereço / Arquivo; Todas / Aguardando). | A joined row of bordered cells. Selected cell: cyan outline, cyan text, 10 % tint. No fill. | Filled cyan (that is the old "pressed = on"). |
| **Tab** | A place (view tabs, open document). Sidebar entries are tabs too. | View tab: dim text, selected = cyan text plus a 2 px cyan underline. Sidebar entry: selected = 3 px cyan bar on the left, cyan text, 10 % tint. | A box. |

## 6. Ningo's voice line, and how it is set apart

The voice line is the one place the interface speaks as Ningo. It is **not** the wordmark's twin: it has
no ghost, no glow, no colour, no bar.

| Property | Value |
|---|---|
| Face | Bodoni Moda 700 italic (the wordmark's face), `--fs-voice` 20 px, line 1.3 |
| Colour | `--ink`. Never cyan, never a tone. |
| Slot | First element under the header, above any toolbar, left-aligned with the body (in Conversa, aligned with the composer column). A fixed place, so it needs no marker. |
| Companion | Always followed by a plain B612 sentence (`.voice-next`, 13 px, `--ink-dim`) that says what to do next. The voice never carries the only copy of a fact. |
| Count | At most one per view. Appears: Conversa on a new conversation, Biblioteca on an empty shelf and on "nothing on that" in search, Leituras when empty, Caderno when something waits, Mensagens on an empty channel. Never in Sistema. |
| Content | First person, masculine in pt where it matters ("o Ningo"). No numbers, no states, no buttons, no adjective that varies by gender. |
| Motion | Terminal print once on first mount (0.8 s); static under reduced motion. |
| Accessibility | A visually hidden "Ningo: " prefix so a screen reader attributes it. |
| Interaction | None. It is never a button or a link. |

The two registers side by side (Caderno):

```
Parte do que tentei aguarda seu veredito.                     <- voice: Bodoni italic, ink
Escolha uma experiência aguardando para dar o veredito.       <- plain: B612, dim
Histórico   7 registradas · 4 aguardando · ...                <- plain: B612, ink
```

**Ningo's transcript replies are not voice lines.** They are long, so they stay in B612 at 15 px,
68 ch, and are set apart by structure: a 2 px `--edge` rule on the left, the author "Ningo" in bold ink,
and the owner's own turns in `--ink-dim` with no rule. The messages Ningo sent into a channel use the same
rule. System text is never in the voice register (the whole of Sistema is plain).

## 7. Layout

```
page (padding 1rem, scanlines + noise, never scrolls)
 |-- sidebar panel 13.5rem -- wordmark, 4 entries, Sistema at the foot (border-top rule)
 `-- frame (container "ws", min 0)
      `-- panel (chamfered, clip-path), legend cut into the top edge
           |-- header    3.25rem: view tabs (left) | refresh icon + at most one primary button (right)
           |-- voice     0 or 1, with its plain sentence
           |-- toolbar   0..2 rows: search, quiet counts, chips, filters
           |-- main      body (scrolls) | aside 22rem (inline >= 62rem, overlay below)
           `-- dock      composer (Conversa, Mensagens) or none
```

- **The page does not scroll.** Only the body (and the aside) scroll, so the dock stays put and a long
  list never moves the composer.
- **Legend.** Cut into the top edge with the page's own scanline and noise layers (`background-attachment:
  fixed`), so the patch matches the page. It needs the page to be fixed-height, which it is.
- **Aside** is a drawer on the right: Add (Biblioteca), experience detail (Caderno), module logs and
  details (Sistema). Inline at panel width >= 62 rem; below that an overlay under the header, 22 rem, with
  a shadow. Esc closes it and returns focus to what opened it. While its own aside is open the opener
  button drops from primary to secondary, so the aside's button is the one primary.
- **Widths.** At 1280 the panel is 1016 px and the aside is inline. At 1024 the panel is 760 px and the
  aside overlays; tables truncate with an ellipsis (title), the dock stacks its buttons under the field,
  and the channel rail goes from 13 to 12 rem.
- **Heights.** At 1280 x 800 the panel is 768 px; at 1024 x 700, 668 px. Both hold header, toolbar and
  about 10 rows before the body scrolls.

## 8. Component inventory

Class names are the mockup's. "n/a" means the state does not exist for that component.

### 8.1 Shell

| Component | Anatomy | Default | Hover | Focus | Selected | Disabled | Loading | Empty / error / off |
|---|---|---|---|---|---|---|---|---|
| **WorkspacePanel** `.frame > .panel.wp` | legend, `.wp-head`, `.voice-block`, `.wp-tools`, `.wp-main` (`.wp-body` + `.wp-aside`), `.wp-dock` | Chamfered, `--panel`, `--edge` edge | n/a | Body is a tab stop only when it holds nothing focusable | n/a | n/a | Cursor and printed rows | Body shows EmptyState, InlineNotice (error, off, offline) |
| **SidebarItem** `.tab` | label, shape, optional count badge, optional word (`.tab-word`) | Dim label, shape in its tone | Ink label, `--wash` | Inset 2 px ink ring | 3 px cyan bar, cyan label, cyan 10 % tint | n/a | Blinking shape | Problem: `▲ 3` badge and "precisam de atenção". Not checked: amber diamond and word. Offline: dash and "sem conexão" on every entry. Stale: shape at 50 % and "desatualizado" |
| **Wordmark** `.wordmark` | Bodoni 700 italic 48 px, cyan and magenta ghosts at -/+ `--ghost` px | `tuning` 3 px | n/a | Ink ring (it is a link to Conversa) | n/a | n/a | `tuning` | `calm` 0, `unwell` 6, `lost` 9, `stale` 3 px at 60 % |
| **ViewTabs** `.vtabs > .vt` | fixed tabs, `.vt-sep`, `.vt-pair` (item tab + `.vt-close`) | Dim 15 px | Ink | 2 px ink ring | Cyan text, 2 px cyan underline | n/a | Blinking cursor after the label | Item tab truncates with an ellipsis and `title`; `x` hover ink on `--wash-strong` |

### 8.2 Controls

| Component | Default | Hover | Focus | Selected / on | Disabled | Loading / busy | Error / off / other |
|---|---|---|---|---|---|---|---|
| **Button** `.btn` | Secondary, see section 5 | Border to ink, `--wash`; primary brightens | 2 px ink ring | n/a (use Switch or Segmented) | Dashed border, dim, `not-allowed`, reason in `title` and as helper text when the cause is not obvious | `aria-busy`, label then a blinking block | Confirming a destructive action: magenta border and text, "Apagar documento?" next to "Cancelar" (the only magenta button) |
| **Switch** `.switch` | Off look | Border to ink | Ring on the whole control | Cyan, word "Ligado" | Dashed track, 60 % | Thumb centred and blinking, word "Aplicando…" | Not checked: amber dashed, "Sem verificação" |
| **SegmentedControl** `.seg` | Dim text, `--edge` borders | Ink text, `--wash` | Ring above neighbours | Cyan outline, tint, count cyan | Dashed | n/a | n/a |
| **Field** `.input` | `--static` well, `--edge` border, ink text, dim placeholder | Border to dim ink | 2 px ink ring, border ink | n/a | Dashed, 70 % | n/a | `aria-invalid`: magenta border and a `.field-error` line below |
| **IntervalField** `.interval` | "A cada [6] [horas]" | as Field | as Field | n/a | as Field | n/a | Edited: `[Aplicar]` appears. Custom: chip "personalizado" and "Voltar ao padrão". Invalid: Field error |
| **Chip** `.chip` | 1 px `--edge`, xs, dim text | n/a | `x` has its own ring | n/a | Dashed | n/a | Warning: amber diamond and amber-tinted border, ink text |

### 8.3 State and messages

| Component | Default | Hover | Focus | Selected | Disabled | Loading | Empty / error / off |
|---|---|---|---|---|---|---|---|
| **StateWord** `.sw` | Shape and word, section 4 | n/a | n/a | n/a | n/a | Blinking square, "Iniciando…" | Unknown: em dash |
| **VoiceLine** `.voice` | Section 6 | n/a | n/a | n/a | n/a | Prints once | Hidden after the first interaction in Conversa; two lengths wrap at 44 rem |
| **InlineNotice** `.notice` | 3 px left bar in the tone, 12 % wash, bold state word in the tone, ink sentence, optional actions on the right | n/a | n/a | n/a | n/a | n/a | `ok`, `other` (info, dash), `unchecked`, `problem` (`role="alert"`) |
| **EmptyState** `.empty` | Voice line, plain next step, optional action or drop target | n/a | n/a | n/a | n/a | n/a | Empty: voice + next step. Off: InlineNotice with "Abrir Sistema". Offline: InlineNotice "O Ningo não está respondendo" + "Abrir Sistema". Error: InlineNotice + "Tentar de novo" |
| **Loading** | A dim line with a blinking block cursor, or printed rows | n/a | n/a | n/a | n/a | the print | n/a |

### 8.4 Data

| Component | Default | Hover | Focus | Selected | Disabled | Loading | Empty / error / off |
|---|---|---|---|---|---|---|---|
| **DataTable** `.tbl` | 36 px rows, hairlines, xs sticky header, dim secondary columns | `--wash` on the row (not on rows that hold controls) | Row link has its own ring; one tab stop for the list, arrows move rows | Cyan 9 % tint and a 3 px cyan bar on the first cell | n/a | Printed rows | Cell placeholder "—"; empty table shows EmptyState; error row shows a notice |
| **DataRow** (experiences) `.exp` | Shape, title, state line, relative time | `--wash` | Inset ring | Cyan tint and bar (`aria-current`) | n/a | n/a | n/a |
| **ContextMenu** `.menu` | `--panel`, `--edge`, shadow, items 13 px | `--wash-strong` | Item ring inset, arrows move, Esc closes and returns focus to the row | n/a | Item dim, dashed | n/a | Destructive item ends in an ellipsis ("Apagar…"): the native confirmation follows |
| **Drawer** `.wp-aside` | Header with title and close, scrolling body | n/a | Focus moves to the close button on open, returns on close | n/a | n/a | Cursor line in the body | Notice in the body |
| **Popover** `.popover` | Opens upward from its button, 27 rem | n/a | Focus moves to the first tile; Esc returns to the button | n/a | n/a | Cursor line | Empty: "Nenhum meme na fila." |
| **MemeTile** `.meme` | 4:3, striped placeholder, caption | Border to ink | Ring | Cyan border plus a cyan check badge (shape, not only colour) | Dashed, "sem prévia" | n/a | Failed preview = disabled |
| **Pager / LoadMore** `.pager`, `.more-line` | "1-14 de 23" and two icon buttons; "Mostrar mais 8" and "atualizado há 6 min" | as Button | as Button | n/a | Previous disabled on page one, next on the last | "Carregando" and a cursor | n/a |

### 8.5 Conversation and settings

| Component | Default | Hover | Focus | Selected | Disabled | Loading | Empty / error / off |
|---|---|---|---|---|---|---|---|
| **Transcript** `.transcript` | Turns with author and time, dividers, anchored at the latest turn, `role="log"` | n/a | Region is a tab stop (nothing focusable inside) | n/a | n/a | "Pensando…" and a cursor | Empty: voice line and starters. Error turn: notice with "Tentar de novo". Notice: divider plus a quiet line ("O Ningo só tem o resumo do feed...") |
| **Composer** `.composer`, `.dock-grid` | Field, context or attachment chips above, primary send on the right, helper "Enter envia · Shift+Enter quebra a linha" | as Field and Button | as Field | n/a | Dashed field and send, helper states the reason ("Indisponível enquanto o Ningo não responde.") | Send `aria-busy` | Error: notice with retry. Uncertain delivery: notice "Pode ter sido enviada: confira antes de reenviar." (copy to add) |
| **FieldRow / SettingsGrid** `.fields > .f` | `label | value | [Editar]` on a shared subgrid; values said in words (Sim, Abertas, "padrão") | n/a | Button ring | Editing row has `--wash` and an inline field with [Revisar] and Cancelar | Read-only: dim value "Fixado para esta versão." and no button. Reviewing and saving: the existing review step, [Salvar e recriar]. Error: Field error |

## 9. Accessibility

### 9.1 Contrast (computed with a script, not read off the CSS comment)

Backgrounds: `--panel` `#0a1119` (everything inside a panel, the sidebar included, which is now opaque);
`--static` `#101b25`; and the **lightest page pixel**, `--static` plus the noise layer at its maximum 7 %
alpha toward `#bfe0ff`, which is `#1c2934`. Scanline rows are darker than `--static`, so they only help.

| Text on | `--ink` | `--ink-dim` | `--cyan` | `--magenta-text` | `--amber` | raw `--magenta` | `--off` |
|---|---|---|---|---|---|---|---|
| panel | 14.88 | 7.28 | 12.69 | 7.10 | 10.53 | 5.68 | **4.04 (fail)** |
| static (input well) | 13.66 | 6.68 | 11.65 | 6.52 | 9.67 | 5.22 | 3.71 |
| lightest page pixel (legend) | 11.63 | 5.69 | 9.92 | 5.55 | 8.23 | **4.44 (fail)** | 3.16 |
| panel + cyan 10 % (selected) | 12.19 | 5.96 | 10.39 | 5.81 | 8.63 | 4.66 | 3.31 |
| panel + cyan 12 % | 11.64 | 5.69 | 9.92 | 5.55 | 8.24 | 4.45 | 3.16 |
| panel + magenta 12 % (problem row) | 13.39 | 6.55 | 11.42 | 6.39 | 9.48 | 5.11 | 3.64 |
| panel + amber 12 % (unchecked row) | 12.15 | 5.94 | 10.36 | 5.80 | 8.60 | 4.64 | 3.30 |
| panel + ink 6 % (hover) | 13.20 | 6.46 | 11.26 | 6.30 | 9.34 | 5.04 | 3.59 |

Every text colour in use (`--ink`, `--ink-dim`, `--cyan`, `--magenta-text`, `--amber`) passes 4.5:1 on every
surface above (lowest: `--ink-dim` 5.69 on the lightest page pixel and on a cyan 12 % tint). Two do not:
`--off` as text (4.04 on the panel) and raw `--magenta` as text on the page (4.44). **They are banned as
text**; they only colour shapes and fills.

Text on fills: panel on `--ink` (primary button) 14.88; panel on cyan 12.69; `--static` on magenta (the
count badge, 12 px bold) 5.22; panel on amber 10.53; the check badge (panel on cyan) 12.69.

Non-text (3:1): focus ring `--ink` on panel 14.88, on the lightest page pixel 11.63. Control borders:
`--edge` on panel **3.28**, on an input well 3.01 (thin). `--off` shapes on panel 4.04, on the lightest
page pixel 3.16. Cyan, magenta and amber shapes are above 8, 5.7 and 10. Known weaker spots:

- `--edge` on a hovered row (2.91) is under 3:1, so rows that hold controls are not washed on hover
  (the Sistema tables), and borders are never drawn on a tinted surface.
- `--edge` on the page itself (2.56) is only the panel's outer edge and the sidebar divider, which are
  decoration; nothing is identified by it alone.
- Disabled controls are exempt; they use a dashed border and `not-allowed` so they do not rely on
  contrast (dim text at 70 % is 4.11 on the panel).
- Placeholders use full `--ink-dim` (6.68 on the well), not a faded copy (4.75 at best).

### 9.2 Focus and keyboard

- One ring: 2 px solid `--ink`, offset 2 px (1 px in fields, -2 px inset on rows, menu items and the
  sidebar so it is not clipped). Verified on the primary button, search field, row link, sidebar tab and
  view tab: `:focus-visible` matches and the computed outline is `2px solid rgb(214, 230, 243)`.
- **Order is DOM order and matches the visual order:** skip link, wordmark, the sidebar (one stop: the
  selected tab; arrows move and select, Home/End), the panel header (view tabs, open-item close, refresh,
  primary), toolbar, body, aside, dock. No positive `tabindex` anywhere (checked: 0).
- Tab lists and segmented groups use roving tabindex; the Documentos list is one stop (arrows move rows,
  then Tab reaches the `⋯` of the active row; Enter opens; Menu or Shift+F10 or right click open the row
  menu). The experience list is one stop. The three Sistema tables are `role="grid"`: one stop, arrows
  move across cells and rows.
- Tab stops per view (checked in the mockup): Conversa 8, Biblioteca > Documentos 13, Leituras 12,
  Caderno 13, Mensagens 11, Sistema > Status about 20, Configurações 16. A scroll region is a stop only
  when it contains nothing focusable.
- Esc closes the menu, then the popover, then the aside, and returns focus to what opened it.
- Shortcuts from the proposal (Ctrl+1..5 done; Alt+1..3 for views and Ctrl+K still open) are not
  represented beyond Ctrl+1..5.

### 9.3 Other

- **Reduced motion:** every animation and transition is off under `prefers-reduced-motion`.
- **Forced colors:** shapes keep their colour (`forced-color-adjust: none`); the count badge falls back to
  system colours; buttons and tabs keep borders and underlines so they survive.
- **Names:** every icon-only button has an `aria-label`; sidebar entries carry the state in their name
  ("Mensagens, aguardando vínculo do WhatsApp"); switches are `role="switch"` with `aria-checked`
  (`mixed` for not checked); live regions: transcripts and message lists are `role="log"`, error
  notices are `role="alert"`.
- **Targets:** 32 px icon buttons, 36 px rows; minimum 24 px.

## 10. Mapping to the current CSS (for the build)

| Now | Becomes |
|---|---|
| `.work-area`, `.work-area .card`, `.card`, `.card-kind` | `.frame > .panel.wp` + `.legend`; the work area is fixed height and does not scroll |
| `.card footer` (Atualizar), `.pager` buttons "anteriores/próximos" | one header refresh icon; `Pager` icon buttons |
| `button:not([role="tab"])` cyan outline, `button[aria-pressed="true"]` cyan fill, `button[role="switch"][aria-checked="true"]` cyan fill | `.btn` variants, `.seg`, `.switch` (section 5) |
| `.tab` text-shadow and `.tab-dot` box-shadow, `.strip span` box-shadow | removed (P3) |
| `.tab[aria-selected]` cyan | kept (bar and tint), without glow |
| `.services`, `.jobs`, `.runtime-modules`, `.module-options` | Sistema `.tbl.parts`, `.attn`, `.jobs`, aside |
| `.conversation`, `.assistant-*`, `.message-compose` | `.transcript`, `.turn`, `.composer`, `.dock-grid` |
| `.view-tabs`, `.view-tab-pair`, `.knowledge-views` | `.vtabs` with `.vt`, `.vt-sep`, `.vt-pair` |
| `.messenger`, `.channels` | `.messenger`, `.rail` (13 rem, 12 under 50 rem) |
| `--muted` (used by `.assistant-turns` and `.assistant-conversation textarea`) | **undefined today**: those borders are silently dropped. Remove with the old classes. |
| `rgb(39 215 228 / 6%)` in `.assistant-prompts` | off-palette literal, dropped with the class |
| `#fff` in `.link-code` | stays (a QR code must be black on white) |
| `font-size: 0.9375rem` body | `--fs-base`; tables, buttons and fields drop to `--fs-sm` |

Implementation notes: panels use `clip-path`, so anything that must overflow the edge (the legend) is
outside the clipped element; menus and popovers live inside the panel (they are clipped by its corners,
which is fine); the aside overlay is positioned under the header; the aside breakpoint is a container
query on the frame, not a media query. The mockup's JS only switches views and runs the roving lists; the
real app already has the tablist code in `sidebar.tsx`.

## 11. Open points

- **Primary for "Aceitar".** The three verdict buttons in Caderno are equal choices, so none is primary; the
  one primary in that view is absent. Say if "Aceitar" should be.
- **"Reiniciar".** The glossary has Iniciar and Parar only. A module that runs but does not answer
  ("Sem resposta") is offered Parar here; a restart verb would read better and is a copy and runtime
  decision.
- **Page size.** The Documentos pager reads "1-14 de 23" because 14 rows fit a 1280 x 800 panel without
  scrolling; the proposal said 20. Either works.
- **Minimum window.** Below about 960 px wide the sidebar-plus-panel layout is untested; the current
  strip-on-top rule for narrow windows is not carried into the mockup.
- **Icons** are inline SVG drawn for the mockup (refresh, plus, close, dots, search, chevron, prev,
  next) because the bundled font is a latin subset. No icon package is proposed.
- **Alt+1..3 and Ctrl+K** remain as the proposal's recommended option; the UI does not show key hints.
- **Copy to update in `ux-proposal.md` section 5** (P7): plain strings say Biblioteca; "Ningo" takes the
  article in pt ("o Ningo não está respondendo", "Pergunte ao Ningo…", "Conversar com o Ningo"); "Enviar
  como Ningo" keeps no article (it is a role).
- **Data the mockup assumes the shell already has:** the indexed count per document, the experience
  state and relative time, the source reason, the status per service. Nothing new is asked of the
  `Transport`. The "sem conexão" and "desatualizado" sidebar states need the shell-level status provider
  from PR 1 of the proposal.

## 12. Screenshots

Taken from the mockup at 1280 x 800 and 1024 x 700 with `prefers-reduced-motion: reduce` (so rows are
fully printed), in `screenshots/`:

| View | 1280 | 1024 |
|---|---|---|
| Conversa with an exchange | `ui-conversa-1280.webp` | `ui-conversa-1024.webp` |
| Conversa, new conversation (voice line) | `ui-conversa-vazia-1280.webp` | `ui-conversa-vazia-1024.webp` |
| Conversa, orchestrator unreachable | `ui-conversa-offline-1280.webp` | |
| Biblioteca > Documentos | `ui-biblioteca-documentos-1280.webp` | `ui-biblioteca-documentos-1024.webp` |
| Biblioteca, Add drawer open | `ui-biblioteca-adicionar-1280.webp` | `ui-biblioteca-adicionar-1024.webp` |
| Biblioteca > Leituras | `ui-biblioteca-leituras-1280.webp` | `ui-biblioteca-leituras-1024.webp` |
| Biblioteca, an open document | `ui-biblioteca-documento-1280.webp` | |
| Biblioteca, empty shelf (voice line) | `ui-biblioteca-vazia-1280.webp` | |
| Caderno | `ui-caderno-1280.webp` | `ui-caderno-1024.webp` |
| Caderno, detail aside open | `ui-caderno-detalhe-1280.webp` | `ui-caderno-detalhe-1024.webp` |
| Mensagens | `ui-mensagens-1280.webp` | `ui-mensagens-1024.webp` |
| Mensagens, meme picker open | `ui-mensagens-meme-1280.webp` | `ui-mensagens-meme-1024.webp` |
| Mensagens, WhatsApp not linked (off) | `ui-mensagens-whatsapp-1280.webp` | |
| Sistema > Status | `ui-sistema-status-1280.webp` | `ui-sistema-status-1024.webp` |
| Sistema, module details aside | `ui-sistema-detalhe-1280.webp` | `ui-sistema-detalhe-1024.webp` |
| Sistema > Status, orchestrator unreachable (the fallback front door) | `ui-sistema-offline-1280.webp` | |
| Sistema > Configurações | `ui-sistema-configuracoes-1280.webp` | `ui-sistema-configuracoes-1024.webp` |
| Sistema > Provedores | `ui-sistema-provedores-1280.webp` | |
| Component catalog (four scroll positions) | `ui-componentes-1-1280.webp` ... `ui-componentes-4-1280.webp` | |

Before and after, side by side: `03-ningo-empty-pt.webp` / `04-ningo-conversation-pt.webp` vs
`ui-conversa-*`; `06-knowledge-docs-loaded-pt.webp` vs `ui-biblioteca-documentos-*`;
`07-knowledge-sources-pt.webp` vs `ui-biblioteca-leituras-*`; `09-memory-experiences-open-pt.webp` vs
`ui-caderno-*`; `10-messages-pt.webp`, `11-messages-memes-pt.webp` vs `ui-mensagens-*`;
`01-system-viewport-pt.webp`, `02-system-full-pt.webp`, `13-system-providers-pt.webp` vs `ui-sistema-*`.

## 13. What was verified

| Check | Result |
|---|---|
| Contrast, computed | Section 9.1. All text colours in use pass 4.5:1 on every surface; `--off` and raw `--magenta` are banned as text. |
| Focus visible | `:focus-visible` true and the 2 px ink ring computed on five control types; one ring screenshot checked at 2x. |
| Keyboard order | Listed per view from the DOM in the mockup; no positive tabindex; roving lists and grids tested with synthetic arrow keys. A real Tab walk was not possible with the tools available (no key press), so order was verified from the DOM, not by pressing Tab. |
| Reduced motion | Emulated: zero running animations, content fully visible. |
| Forced colors | Emulated: usable; one fix made (count badge). |
| Viewports | `emulate` at 1280x800x1 and 1024x700x1; `innerWidth` and `innerHeight` confirmed before capture. At 1024 the Caderno and Mensagens layouts do not crush: the aside overlays and the dock stacks. |
| Hygiene | No `/home`, `/tmp`, `node_modules`, external URL (only the SVG namespace the policy allows), real id or avoided term in the files written here. |
