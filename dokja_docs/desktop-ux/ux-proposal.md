# Ningo desktop: UX audit and redesign proposal

Date: 2026-10-23. Author: UX designer agent. Scope: `dokja_interfaces/desktop` renderer only.
Screenshots (taken with `pnpm dev:web` fake data) are in `screenshots/`.

How it was checked: `pnpm dev:web` (fake data), every workspace and sub-view in pt and the three
main ones in en, a knowledge search, a document tab, an experience detail, the meme picker, the
module "Opções" disclosures, plus a read of the code. Limits: the fake data has no empty states
(copy for those comes from the locale keys); `dev:web` forces `runtimeControls: true`, so the
shipped no-runtime System shape (`health.tsx:140-152`) is described from code; the Providers
inventory cannot be seen offline (`fake_transport.ts` never answers `runtime.providers`, so it shows
"unexpected reply"; that is a dev-data gap, not a product bug); narrow windows and Electron's own
menu were not tested.

## Owner decisions (2026-10-23)

These answer the open questions in §9 and win over the proposal where they differ.

- **Front door:** the screen opens on Conversa. When the orchestrator cannot be reached, it opens
  on System, showing what is wrong.
- **Names:** the workspaces are Conversa, Biblioteca (knowledge), Caderno (memory), Mensagens and,
  at the foot, Sistema. In en: Talk, Library, Notebook, Messages, System.
- **Voice:** Ningo speaks in the first person and is masculine in pt ("o Ningo"). At most one
  line of Ningo's voice per view, followed by a plain next step; System stays plain.
- **Composer:** docked only in Conversa for now; no global "ask Ningo" bar.
- **System:** split into three sub-views, Status, Configurações and Provedores.
- Still open: the Alt+1..3 and Ctrl+K shortcuts (§9); the recommended option applies until the
  owner says otherwise.

---

## 0. Corrections to the first pass

1. **The frame is a recorded decision, not a bug.** Plan, Phase C: "The cards keep their chamfered
   panel ... the panel's legend names the workspace" (owner, 2026-10-19). What is actually wrong is
   several frames per view (System stacks three), a frame under a strip of pressed buttons
   (Knowledge), the legend repeating the sidebar word, and a Refresh footer in each. Proposal: one
   chamfered panel per workspace, legend used once as a breadcrumb. No decision is reversed.
2. **"Mensagens recentes" is not a load gate.** History loads on open (`communications.tsx:80-86`).
   The button is a manual refresh that duplicates the auto-refresh checkbox and the card footer
   Refresh: three refresh controls on one screen. The real gates are in problem 5.
3. **"Opens on System" has a cause the first pass did not name.** Changing `START` is not enough:
   see problem 2. `HealthCard` is the only thing that reads `ningo.status`, so the sidebar dots,
   the problem count and the wordmark pulse are blind until System is opened.
4. The chat workspace exists (first sidebar entry, `message.created` is allow-listed), but the plan
   (Phase B) and the README still describe it as gated, and the README still says "four
   workspaces", "opens on System" and "System is Ctrl+4" (it is Ctrl+5).

---

## 1. Ranked problems

Ranked by harm to the owner's main jobs: (A) talk to Ningo, (B) find/read/add knowledge,
(C) check what Ningo remembers and how its guesses fared, (D) read and answer a channel,
(E) see what is broken and fix it.

| # | Problem | Evidence | Principle broken | Job |
|---|---|---|---|---|
| 1 | **The problem count leads nowhere and System contradicts itself.** The sidebar says `sistema 3`, but the System view shows no red row. Each module row says "Desligado" (container stopped), and only inside the collapsed 6 pt "Opções de chat_ai" is there "Atividade: erro · connection refused" and a cyan switch reading "ligado". Two meanings of "on" (container lifecycle vs `services.set` routing) share the words Ligar/ligado/Desligado in pt, and `status_stopped` ("stopped", a problem) shares a word with `runtime_stopped` ("Stopped", just off). | `12-system-options-open-pt.webp`, `15-system-en.webp`, `01-system-viewport-pt.webp`; `runtime.tsx:77-103`; `health.tsx:132-137, 168-172` (managed rows are filtered out of the visible list); `pt.json:15,17,296-305` | Visibility of system status; the owner must never decode whether something is on | E |
| 2 | **Front door is the back room, for a hidden reason.** `START = "health"` (`registry.ts:36`) is required because `HealthCard` is the only requester of `ningo.status` and pushes `report`/`reportTones` (`health.tsx:94-98`). Changing START alone leaves dots blank and the wordmark stuck at "tuning". The page the owner lands on is ~2700 px tall (`02-system-full-pt.webp`). | `registry.ts:36`, `health.tsx:94-98`, `sidebar.tsx:119-125`, `pulse.tsx:6-12` | Ningo is someone, not a dashboard | A |
| 3 | **The conversation is a form in a card.** Composer sits inline under the transcript and moves down as it grows; full-width "Enviar" that looks disabled; textarea overflows the panel padding (right edge at 1143 vs buttons at 1123); two intros stacked ("Traga uma pergunta..." and "Pergunte, explore..."); starter chips are written in the owner's voice and send immediately; "Começar nova conversa" is a peer button; id regenerated on every mount; no scroll model. Coach tone, nothing of Ningo. A second chat lives inside each Sources row, with its own composer. | `03-ningo-empty-pt.webp`, `04-ningo-conversation-pt.webp`, `16-ningo-en.webp`; `assistant_conversation.tsx:14,44-62`; `assistant.tsx:9-15`; `digest.tsx:172-177`; `en.json:324-347` | Docked composer (replan); one conversation surface; persona | A |
| 4 | **No shared template.** Five shapes: chat card; Knowledge = pressed-button pair above a card; Memory = one card; Messages = card with two panes; System = three stacked cards. The whole window scrolls (`.workspace`/`.work-area`, `styles.css:79-103`), so nothing can dock. Knowledge has three navigation levels and two tab idioms (sidebar tabs, `aria-pressed` buttons "documentos/fontes", underline tabs "Base de pesquisa / doc ×"); "Base de pesquisa" is both the h2 and the first tab. | `05`, `07`, `08`, `10` png; `knowledge_workspace.tsx:28-34`; `knowledge.tsx:147-173` | One layout template | all |
| 5 | **Content behind load buttons**: "Carregar documentos" (`knowledge_documents.tsx:72-75`), "Carregar experiências" (`memory_experiences.tsx:36`), "Carregar memes" (`communications.tsx:173`), "Inspecionar provedores" (`providers.tsx:19`). Refresh is repeated: footer "Atualizar" in every card (`card_frame.tsx:65-69`), three in Messages, and the one inside a document tab reloads the whole base (`document_view.tsx:72`). | `05`, `06`, `08`, `10`, `13` png | Content visible on open; recognition over recall | B, C, D |
| 6 | **Knowledge has the wrong weight.** Order is status lines, "Indexar trechos pendentes", search, an eight-field add form, then the document list behind a button (`knowledge.tsx:174-179`). The job "find and read" starts below the fold; the job "add" owns the page. | `05-knowledge-documents-pt.webp`, `06` | One primary action per view | B |
| 7 | **Study views show telemetry, and raw values leak.** Memory rows show `hashtag.suggest`, `fixture:experience-1`, ISO `2026-10-21T10:00:00Z` (skips `ago()`), and the status "Pendentes" (the filter's plural) as a row state; the detail opens with a wall of caveats. Documents show `note:example-1 · note` (id then kind again). Messages shows `#reading-room · <channel id>`. Settings show `true`, `open`, `<dígitos>@g.us`. Names `orchestrator`, `chat_ai`, `meme.refresh`. "embedding", "baseline", "Brier", "skill" are English in pt. Error copy says "card" ("este card espera", `en.json:35`). | `09`, `06`, `10`, `02` png; `memory_experiences.tsx:56-58`; `knowledge_documents.tsx:85`; `document_view.tsx:51`; `communications.tsx:46-49`; `pt.json:49-51` | Match the real world; voice vs plain | B, C |
| 8 | **Four on/off idioms, no primary button.** (a) "Ligar/Start" buttons styled with the `.switch` class (`runtime.tsx:83`); (b) cyan `role="switch"` fill (`health.tsx:393`, `styles.css:292`); (c) any `aria-pressed` button painted like ON (`styles.css:542`): the selected "documentos" tab and the "Nota" mode look exactly like a switch that is on; (d) bare checkboxes (auto-refresh, force send, provider discovery). One button style for everything (`styles.css:365`). Settings "Editar" buttons start at three different x positions; the interval box is an unstyled number field. | `05`, `02`, `11` png | Consistency; one meaning per control | E, B |
| 9 | **One view, four names.** The sub-tab says "fontes", the legend "leituras", the title "Resumo de leituras", the code "digest". It shows readings (items to discuss); the actual sources are a disclosure at the bottom. | `07-knowledge-sources-pt.webp`; `knowledge_workspace.tsx:13-16`; `digest.tsx:67` | One name per thing | B |
| 10 | **Replan decisions not realised.** Composer docked at the bottom: no. Context menus on rows: none exist (`grep contextmenu` is empty); every row has its own Abrir/Apagar buttons. Sidebar "a dot and a word": only a colour dot. | `06` png; `sidebar.tsx:118-126` | Recorded decisions | B |
| 11 | **Layout waste and mis-weighted tiles.** Panel capped at 64 rem and left-aligned (`styles.css:101-103`): ~40 % of a 1920 px window is empty. Messages' channel rail takes about a third of the card for one channel. Each meme tile's "Anexar" button is as large as its thumbnail. | `10`, `11` png | Fewer, deliberate elements | D |
| 12 | **Repeated chrome.** Legend repeats the sidebar word on every card (`card_frame.tsx:59`, `assistant.tsx:10`, `providers.tsx:16`: "sistema" three times); the wordmark "Ningo" sits directly above a nav item "Ningo" (`app.tsx:11-17`, `registry.ts:29`). | `03` png | Confidence, not noise | all |
| 13 | **Persona is absent.** No Ningo voice anywhere except the wordmark; the only warm copy (chat) is a coach. Gender is inconsistent in pt: "Ela vai avisar" (`pt.json:347`) vs "o Ningo" / "pelo Ningo". Capitalisation varies ("Desligado" runtime vs "desligado" switch vs "no ar"). | `pt.json`, `en.json` | Ningo is someone | A |
| 14 | **Accessibility gaps.** Sidebar state is colour only (no word, no shape); nested duplicate region label "Converse com Ningo" (`2_0`/`2_4` in snapshot); sub-view group uses `aria-pressed` instead of a tablist; row actions exist only as per-row buttons (no keyboard menu path). Good already: sidebar tablist with arrows/Home/End, Ctrl+1..5 from fields (`sidebar.tsx:46-85`), reduced-motion media query (`styles.css:421`). | snapshots | Keyboard, ARIA | all |
| 15 | **Doc and test drift.** README/plan out of date (section 0.4); `fake_transport.ts` lacks `runtime.providers` and has no empty states; `tests/ui/app.test.tsx:110`, `tests/e2e/smoke.e2e.ts` (waits for `.services li` at start) assume System opens first. | files named | maintainability | - |

---

## 2. Persona, IA and voice

### What the name asks of the interface

- **Reader.** What Ningo has read is a library: documents on a shelf, readings in progress. Show
  them first, with title, kind, source and indexing as one readable row, not as counts.
- **Only child.** One Ningo, one voice, one conversation surface. No assistant picker, no per-row
  mini chats, no "profiles". Discussing a reading opens *the* conversation with that reading as
  context.
- **Alone.** Ningo runs on its own: the back room (System) is "what keeps it standing on its own",
  plain and exact, with no performance.
- **Arrogant philosopher.** Short, firm, dry. One Ningo line per view at most, never insulting,
  never cute, never a greeting that presents instead of letting the owner work.

### Where Ningo speaks and where the owner operates

| Surface | Register | Why |
|---|---|---|
| Talk: opening line, chip text (owner's words, but written as challenges) | **Voice** (one line) | The conversation is the heart. |
| Knowledge: empty shelf, "nothing on that" search result, empty readings | **Voice + plain next step** | Replan: empty states say what to do next. |
| Memory: the one summary headline, empty state | **Voice** (one line) | The notebook is Ningo's own account. |
| Messages: empty channel only | Voice (one line) | Social presence; everything else is plain because it sends text out under a name. |
| System, Settings, Providers | **Plain, entirely** | Back room. The wordmark ghost is the only flourish. |
| Switch states, errors, confirmations, destructive dialogs, send identity ("as Ningo" / "as you"), counts | **Plain, always** | Never decode a joke to know whether something is on. |

**Typographic rule (hand to ui-designer):** the Ningo voice is set in the wordmark's face (Bodoni
italic, already bundled); everything else stays in B612 Mono. Then voice vs plain is visible at a
glance and the rule enforces itself. A voice line never carries a button or the only copy of a
fact; it always has a plain sentence under it.

Do not: hero tone ("I am Ningo, your..."), gamified scores (calibration is shown as plain numbers,
never as a rank, streak or badge), mascots, jokes in switches or errors, first-person voice in
System.

### Glossary (one word, one meaning, both locales)

| Concept | en | pt |
|---|---|---|
| Container lifecycle (button = verb) | Start / Stop; Running, Starting, Stopped, Failed | Iniciar / Parar; Rodando, Iniciando, Parado, Falhou |
| Routing switch (`services.set`) (state) | Handle requests: On / Off; row state "Paused" | Processar solicitações: Ligado / Desligado; estado "Pausado" |
| Service health (`ningo.status`) | Working, Not answering, Not checked | Funcionando, Sem resposta, Sem verificação |
| `status_stopped` (problem) | Not answering | Sem resposta (no longer "parado") |
| Feed items | Readings | Leituras |
| Feed plugins | Sources | Fontes |
| Embedded passages | "indexed for similarity search" (plain); "embedding" only in the operator hint | "indexados para busca por similaridade"; "embedding" só na dica de operador |
| What Knowledge holds (plain strings) | "knowledge" ("Search knowledge…", "Add to knowledge"); never "research base", "knowledge base" or "library" in plain copy | "conhecimento"; nunca "base de pesquisa", "base de conhecimento" ou "biblioteca" em texto plano |
| Same thing inside a voice line | "shelf" and "library" are allowed only in voice lines | "estante" e "biblioteca" só em linhas de voz |
| Ningo in pt | no article before the name ("Ningo não está respondendo", "Conversar com Ningo", "como Ningo") until question 4 is answered | sem artigo antes do nome até a pergunta 4 ser respondida |
| Memory items | Experiences | Experiências |
| Predicted vs reference | Predicted acceptance, Baseline | Aceitação prevista, Referência (baseline) |

Capitalisation: sentence case everywhere; sidebar and sub-view names capitalised the same way
("Knowledge", not "knowledge"); state words capitalised ("Running"), never mixed with lowercase
variants. Pt: avoid the anglicism "jobs" (use "tarefas"); keep Ningo grammatically neutral
(see open question 6).

### Information architecture

```
Sidebar (Ctrl+1..5)                 Opens on   Sub-views (Alt+1..3, tabs in the panel header)
 1  Talk        (was "Ningo")       THIS       none; one conversation
 2  Knowledge                                  Documents (default) | Readings        + open documents as closable tabs
 3  Memory                                     none; summary + experiences list + detail aside
 4  Messages                                   none; channels rail + thread + dock
 ─────────────
 5  System      [3 problems] foot             Status (default) | Settings | Providers
```

- **Opens first: Talk**, not System. If the status read fails (orchestrator down), Talk still
  opens, shows the offline state with a plain link to System (open question 3).
- **Back room = System.** Everything an operator does lives there and nowhere else: start/stop,
  routing switches, jobs, settings, providers, logs, WhatsApp linking. The study workspaces never
  show container or embedding telemetry beyond one quiet chip.
- **Memes** stay a picker in Messages (owner decision, 2026-10-19). The meme service switch stays
  in System.
- **Sources** stay a view of Knowledge, but renamed so the tab names what it shows: Readings, with
  "Sources" as a collapsed strip inside it.
- **Status ownership (critical, PR 1 in section 7).** One `ningo.status` request, issued by a
  shell-level `StatusProvider` at start, after any System change, and (optionally) on window focus
  after 60 s; Pulse, sidebar dots, the foot count and System all read it. The "one request" rule
  stays; only the owner of the request changes. Do not refresh while a chat turn is in flight (the
  orchestrator answers one request at a time; a chat turn can hold the queue up to 120 s,
  `assistant_conversation.tsx:31`): show the last known state, marked stale.
- **Sidebar entries** get a dot and a word (visible on hover/focus and in `aria-label`: "Memory,
  working"), plus a shape per tone so colour is not the only carrier. Talk gets `services:
  ["chat_ai"]`. When the status request itself fails, the foot shows "offline" instead of nothing.

### Keyboard

Ctrl+1..5 unchanged. Add: Ctrl+K focuses the docked composer (jumps to Talk from anywhere except
Messages, where it focuses the channel composer; confirm); Ctrl+F focuses the toolbar search in
Knowledge; Esc closes aside/drawer/popover or clears search; Menu key / Shift+F10 opens the row
menu; Enter on a row opens it; Alt+1..3 switches sub-views (verify Alt+digit does not collide with
Electron's menu mnemonics on Linux/Windows). Bare keys are never taken (replan rule). Ctrl+W, Ctrl+R
and F5 stay Electron defaults.

---

## 3. The shared workspace template

One chamfered panel per workspace, filling the work area's height. Only the body (and the aside)
scroll, so a dock can stay put.

```
┌ knowledge › documents ───────────────────────────────────────────────────────┐  legend = breadcrumb:
│ [Documents] [Readings] │ [On the freedom of choosing ×]      ⟳   [ + Add ]     │  workspace › view/item
│ (voice line, 0 or 1, Bodoni italic)                                          │  HEADER: view tabs left;
├──────────────────────────────────────────────────────────────────────────────┤  refresh icon + ONE
│ toolbar: search · filters · quiet status chips                  (optional)   │  primary button right
├─────────────────────────────────────────────────────┬────────────────────────┤
│ BODY (scrolls)                                      │ ASIDE (optional)       │
│ list · table · transcript · form                    │ detail · logs · picker │
│ states: loading | empty | error | off | offline     │ 22 rem, Esc closes     │
├─────────────────────────────────────────────────────┴────────────────────────┤
│ DOCK (pinned, optional): composer / status line                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

Rules:

1. One panel, one legend (workspace › view), no per-section cards, no `card-kind` repeat, no
   footer Refresh. Refresh is one icon button in the header (tooltip "Refresh"); data also reloads
   after any change the owner makes.
2. **Content loads when the workspace (or sub-view) first opens**, never behind a button. Lazy
   mounting stays: a workspace nobody opened asks nothing. Heavy views (Providers, meme picker)
   load when their sub-view/popover opens.
3. At most one primary button per view, in the header (Add, Send, Start). Everything else is
   secondary or an icon.
4. Control semantics are fixed: **switch** = a persistent state that applies at once; **button** =
   a verb; **segmented control** = a mode or filter; **tabs** = a place. Nothing else may look like
   a switch.
5. Open items (documents) sit in the same strip as the sub-views, after a separator, closable.
   Two navigation levels in total (sidebar, strip), not three.
6. Width: body is fluid; running text is capped at about 72 ch; the aside is a fixed 22 rem and
   becomes an overlay under about 1000 px.
7. Four states every body must define: loading (existing cursor print), empty (voice line + plain
   next step), error (plain + Try again), off/offline (plain + "Open System").
8. Destructive actions keep the native confirmation; changes still need a real click or key press.

---

## 4. Wireframes

Made-up names only (`example.com`, "#reading-room", "Book club" alias).

### 4.1 Talk

```
┌ talk ───────────────────────────────────────────────────────────────── ⋯ ┐   ⋯ = Start a new conversation
│                                                                           │
│  I've read more than you'd guess. Ask.                    (voice, empty only)
│  [ Argue against: ]  [ What am I missing about: ]  [ Summarize and judge: ]   chips FILL the composer
│                                                                           │
│  ── after the first turn the voice line and chips go away ──              │
│  You                                                           14:02      │
│  What do you make of Nietzsche on resentment?                             │
│  Ningo                                                         14:02      │
│  ...reply text, wraps, selectable...                                      │
│  ───────  Discussing: "An example entry" (new conversation)  ───────      │   divider when started from Readings
│                                                     [ ↓ Jump to latest ]  │
├───────────────────────────────────────────────────────────────────────────┤
│ Discussing: An example entry  ×                         (context chip)    │
│ ┌ Ask Ningo…                                                     ┐ [Send] │   Enter sends, Shift+Enter newline
│ └────────────────────────────────────────────────────────────────┘        │   counter shows only near 2000
└───────────────────────────────────────────────────────────────────────────┘
Offline body:  Ningo is not answering. The orchestrator isn't running or can't be reached.   [ Open System ]
               (composer stays, disabled, with the same reason as its helper text)
```

Notes: the conversation (transcript) lives in a provider above the workspaces so the dock and the
workspace share it. "Discuss with Ningo" on a reading opens this workspace with the reading as the
context chip; it starts a new session id (as today's `key={item.id}` already does) and the
transcript shows a divider. Persisting across restarts needs backend (B1).

### 4.2 Knowledge: Documents (default)

```
┌ knowledge › documents ──────────────────────────────────────────────────┐
│ [Documents] [Readings]                                   ⟳   [ + Add ]   │
├──────────────────────────────────────────────────────────────────────────┤
│ ⌕ Search knowledge…                                           [Search]   │   Enter runs; Esc clears
│ 23 documents · 69 passages   (!) 10 passages not indexed [Index now]  ·  similarity: on │  quiet chips
├──────────────────────────────────────────────────────────────────────────┤
│ Title                          Kind     Source           Indexed  Updated │
│ On the freedom of choosing     note     notes/freedom.md   2/2    2 h ago │  right-click / Menu key / ⋯ :
│ A short history of boredom     article  example.com       0/3 (!) 3 h ago │   Open · Classify… · Delete…
│ …                                                                        │
│ ‹ 1–20 of 23 ›                                                           │
└──────────────────────────────────────────────────────────────────────────┘
Empty shelf: the body shows the voice line and its plain next step (copy table); no voice line otherwise.
Search active: the list is replaced by hits (title, heading, snippet, weak-match marked), "Clear search" at top.
Drag a file anywhere: overlay "Drop to add"; opens the Add drawer on File.
Add drawer (aside, not a page section):  Add to knowledge
   (•Note)(Address)(File)   Title ▢   Text ▢ …   Details ▾ (kind, source, tags)   Advanced ▾ (id)   [Add]
Open document: becomes a tab after a separator [Documents] [Readings] │ [On the freedom… ×]; tab body =
   title, kind · source, "2 of 2 passages indexed", [Classify], rebuilt-text note, text.
```

### 4.3 Knowledge: Readings

```
┌ knowledge › readings ───────────────────────────────────────────────────┐
│ [Documents] [Readings]                                        ⟳          │
│ Nothing is feeding me right now.        (voice, only when empty)         │
├──────────────────────────────────────────────────────────────────────────┤
│ 2 sources working · 1 failing · 1 off                  [ Sources ▾ ]     │   ▾ expands the strip (opens by itself when one fails)
├──────────────────────────────────────────────────────────────────────────┤
│ An example entry, number 1                                                │
│ Local file example · 47 min ago                                           │
│ Two sentences that stand in for the summary…            [Discuss with Ningo]
│ …                                                                         │
│ [ Show 8 more ]   updated 6 min ago                                       │
└──────────────────────────────────────────────────────────────────────────┘
Sources strip: name · state word · items · "updated 7 min ago" · reason (service text, as sent).
```

### 4.4 Memory

```
┌ memory ──────────────────────────────────────────────────────── ⟳ ┐
│ Some of what I tried waits for your verdict.                        │  voice line, no numbers; only when something is pending
├─────────────────────────────────────────────────────────────────────┤
│ Track record   7 recorded · 7 waiting · 0 settled · 0 expired        │
│ Calibration: not enough settled yet (0 of 30).  Prediction 0.081, baseline 0.270 (lower is better) [What's this?]
│ similarity: on                                                       │  quiet chip
├──────────────────────────────────────────┬──────────────────────────┤
│ [All 7] [Waiting 7] [Settled 0]           │ Experience               │
│ Suggested #Reading for a meme             │ Suggested #Reading       │
│   waiting for your verdict · 2 days ago   │ Predicted acceptance 60% │
│ Suggested #Cats for a meme                │ Baseline 50%             │
│   settled: accepted · 5 days ago          │ Similar earlier case 80% │
│ …                                         │ Context  ▾   Earlier example ▾ │
│ ‹ 1–7 of 7 ›                              │ Your verdict:            │
│                                           │ [Accept] [Not used] [Other result… ] │
│                                           │ (one-line rule; "More" ▾ for the long caveat) │
└──────────────────────────────────────────┴──────────────────────────┘
```

The long feedback caveats (`en.json:276-283`) move under a "More" disclosure; the one-line rule
stays visible: "The first verdict is kept. No verdict is not a rejection." The verdict flow and its
native confirmation do not change.

### 4.5 Messages

```
┌ messages ──────────────────────────────────────────── ● Live (15 s) ┐
│ Channels            │ #reading-room · Discord      sent as Ningo       │
│ ▸ #reading-room     │            [ Load earlier messages ]             │
│   Book club (WhatsApp)│ A reader                       1 minute ago    │
│                     │ What have you been reading?                      │
│ (rail collapses to  │ Quiet in here. (voice, empty channel only)       │
│  a dropdown in the  ├──────────────────────────────────────────────────┤
│  header when there  │ [meme chip: Placeholder 4 ×]                     │
│  is one channel)    │ ┌ Message #reading-room as Ningo…        ┐ [Meme] [Send as Ningo]
│                     │ └─────────────────────────────────────────┘      │
└─────────────────────┴──────────────────────────────────────────────────┘
Meme popover (opens upward from [Meme]): grid of thumbnails; click = attach/detach (check badge); pager; no per-tile button.
 Attached meme shows as a chip with ×; hashtag tagging lives in the chip's detail.
WhatsApp thread: a standing line "Goes out from your own account, as you. Text only, a few a minute."; button reads [Send as you].
```

One refresh model: "Live" toggle in the header (off pauses polling), "Load earlier" at the top of
the thread, no footer Refresh, no "Mensagens recentes" button (sending already reloads).

### 4.6 System (plain throughout)

```
┌ system › status ────────────────────────────────────────────────── ⟳ ┐
│ [Status] [Settings] [Providers]                                         │
├─────────────────────────────────────────────────────────────────────────┤
│ Needs attention (3)                                                     │  only when > 0; each row has its fix
│  [x] chat_ai    Not answering: connection refused        [Logs] [Start] │
│  (~) whatsapp   Waiting for link                          [Link…]       │
│  [x] scheduler  Not answering: no announce for 12m       [Logs]         │
├─────────────────────────────────────────────────────────────────────────┤
│ Core                         State      Handle requests   Module        │
│  Orchestrator                ■ Stopped         —           [Start]  ⋯    │
│  Memory                      ● Running       (on)          [Stop]   ⋯    │
│  Knowledge  …                                                            │
│ Communication services                                                   │
│  Discord                     ● connected    (on)           [Stop]   ⋯    │
│  WhatsApp                    (~) waiting for link (on)     [Link…] [Stop] ⋯ │
│ Scheduled jobs                                                           │
│  Refresh memes   (on)   every [ 6 ] [hours ▾]  [Apply]*   last run: never│  * Apply appears only when edited
│  Post memes      (on)   every [ 1 ] [hours ▾]             last run: skipped │
│ Modules keep running after you close the desktop. Stopping keeps their data. │
└─────────────────────────────────────────────────────────────────────────┘
⋯ opens the ASIDE: Logs (live tail) · what starting this will do (delivery notice) · routing hint.
Without runtime controls (shipped shape): same table, minus the Module column; rows come from `ningo.status` only.
Settings view: grouped Discord / WhatsApp / Models; one two-column field grid (label | value | Edit), values localised
 (true → Yes, open → Open), review step and "Save and recreate" unchanged. Providers view: lazy list; short plain caveat.
```

The Parts table is a join, in the renderer, of `runtime.modules` and `ningo.status` by name. One
state word per row; both raw facts are in the aside. This is presentation, not a business rule.

### 4.7 Flows (one per main job)

1. **Talk.** Open the app: Talk is open, composer focused. Type, Enter. "Thinking…" shows in the
   transcript; the reply is appended and the view stays anchored. From any other workspace Ctrl+K
   returns to the composer. If Ningo does not answer: offline body with [Open System].
2. **Add and find knowledge.** Knowledge opens on the document list. Drop a file anywhere (or
   [+ Add]): the Add drawer opens on File with the name filled. [Add]: an inline line says "Added.
   3 passages stored." and the list reloads. If the "not indexed" chip shows, [Index now], counts
   update. To find: Ctrl+F, type, Enter, hits replace the list, Esc clears.
3. **Read and discuss.** Knowledge, Readings, entry, [Discuss with Ningo]: Talk opens with the
   context chip and a divider. Send; the reply appears. × on the chip ends the context. Ctrl+2
   goes back to Readings where it was.
4. **Settle what Ningo tried.** Memory shows the voice line when something is pending. Filter
   Waiting, select a row: the aside shows predicted acceptance against baseline. [Accept], [Not
   used] or "Other result…", then the native confirmation. The row moves to Settled and the
   counts and calibration reload.
5. **Answer a channel.** Messages, pick a channel: the thread is loaded and Live is on. Type;
   optionally [Meme], click a thumbnail, it becomes a chip. [Send as Ningo] (WhatsApp: [Send as
   you]); the native confirmation appears wherever the shell asks for one today; the thread
   reloads.
6. **See what is broken and fix it.** Sidebar badge "3" (or the wordmark ghost drifting) opens
   System on Status. Needs attention lists each problem with its reason and its fix. [Start] on
   chat_ai: the row shows Starting with progress, the status is read again, the row leaves the
   list and the badge drops to 2.

---

## 5. Copy (en and pt)

V = Ningo's voice (Bodoni italic, one line per view). P = plain.

| Where | Type | en | pt |
|---|---|---|---|
| Sidebar: conversation | P | Talk | Conversa |
| Sidebar | P | Knowledge · Memory · Messages · System | Conhecimento · Memória · Mensagens · Sistema |
| Sidebar tooltip/aria | P | "{name}, {state}" e.g. "Memory, working" | "{name}, {estado}" e.g. "Memória, funcionando" |
| Talk opening line | V | I've read more than you'd guess. Ask. | Li mais do que você imagina. Pergunte. |
| Talk composer placeholder | P | Ask Ningo… | Pergunte a Ningo… |
| Talk chips (fill, do not send) | P | Argue against: · What am I missing about: · Summarize and judge: | Argumente contra: · O que estou deixando passar sobre: · Resuma e julgue: |
| Talk thinking | P | Thinking… | Pensando… |
| Talk offline | P | Ningo is not answering. The orchestrator isn't running or can't be reached. | Ningo não está respondendo. O orquestrador não está rodando ou não pode ser alcançado. |
| Talk offline action | P | Open System | Abrir Sistema |
| Reading context chip | P | Discussing: {title} | Conversando sobre: {title} |
| Reading limit note | P | Ningo only has the feed summary of this entry, not the whole article, and says when it needs more. | Ningo só tem o resumo do feed desta leitura, não o artigo inteiro, e avisa quando precisar de mais. |
| New conversation (menu) | P | Start a new conversation | Começar nova conversa |
| Knowledge empty shelf | V | The shelf is empty. I can't judge what I haven't read. | A estante está vazia. Não julgo o que não li. |
| Knowledge empty, next step | P | Drop a file here, or choose Add to paste a note or a web address. | Solte um arquivo aqui, ou use Adicionar para colar uma nota ou um endereço da web. |
| Search placeholder | P | Search knowledge… | Buscar no conhecimento… |
| Search, no hits | V | I have nothing on that yet. | Não tenho nada sobre isso ainda. |
| Search, no hits, next step | P | Add a document or try other words. | Adicione um documento ou tente outras palavras. |
| Counts | P | {documents} documents · {chunks} passages | {documents} documentos · {chunks} trechos |
| Index warning | P | {count} passages are not indexed for similarity search yet. | {count} trechos ainda não foram indexados para a busca por similaridade. |
| Index action | P | Index now | Indexar agora |
| Add button / drawer title | P | Add · Add to knowledge | Adicionar · Adicionar ao conhecimento |
| Row menu | P | Open · Classify… · Delete… | Abrir · Classificar… · Apagar… |
| Sub-views | P | Documents · Readings | Documentos · Leituras |
| Readings empty | V | Nothing is feeding me right now. | Nada está me alimentando agora. |
| Readings empty, next step | P | No source is followed. Add a plugin to the feeds service and its items appear here. | Nenhuma fonte é seguida. Adicione um plugin ao serviço de feeds e os itens aparecem aqui. |
| Sources summary | P | {ok} sources working · {failed} failing · {off} off | {ok} fontes funcionando · {failed} com falha · {off} desativadas |
| Discuss | P | Discuss with Ningo | Conversar com Ningo |
| Memory voice line (only when something is pending; no numbers, they are in the strip below) | V | Some of what I tried waits for your verdict. | Parte do que tentei aguarda seu veredito. |
| Memory empty | V | Nothing recorded. I haven't tried anything yet. | Nada registrado. Ainda não tentei nada. |
| Memory empty, next step | P | Experiences are recorded when Ningo suggests something, such as a hashtag for a meme. | Experiências são registradas quando Ningo sugere algo, como a hashtag de um meme. |
| Memory filters | P | All · Waiting · Settled | Todas · Aguardando · Resolvidas |
| Memory row | P | Suggested {hashtag} for a meme · waiting for your verdict · {ago} | Sugeriu {hashtag} para um meme · aguardando seu veredito · {ago} |
| Calibration, too few | P | Not enough settled yet: {scored} of {needed}. | Poucas resolvidas para julgar: {scored} de {needed}. |
| Brier hint | P | Measures how far predicted probabilities were from what happened. Lower is better. | Mede o quanto as probabilidades previstas ficaram longe do que aconteceu. Menor é melhor. |
| Verdict rule | P | The first verdict is kept. No verdict is not a rejection. | O primeiro veredito é mantido. Sem veredito não é rejeição. |
| Messages empty channel | V | Quiet in here. | Silêncio por aqui. |
| Messages empty, plain | P | No messages in this channel yet. | Ainda não há mensagens neste canal. |
| Messages no channels | P | No delivery channels are configured. | Nenhum canal de entrega configurado. |
| Composer placeholder | P | Message {channel} as Ningo… | Mensagem para {channel}, enviada como Ningo… |
| Send buttons | P | Send as Ningo · Send as you | Enviar como Ningo · Enviar como você |
| WhatsApp standing line | P | Goes out from your own account, as you. Text only, a few a minute. | Sai da sua própria conta, como você. Só texto, no máximo algumas por minuto. |
| Meme button / picker | P | Meme · Pick a meme · No memes waiting. | Meme · Escolha um meme · Nenhum meme na fila. |
| Live toggle | P | Live (every 15 s) | Ao vivo (a cada 15 s) |
| Load earlier | P | Load earlier messages | Carregar mensagens anteriores |
| System sections | P | Needs attention · Core · Communication services · Scheduled jobs | Precisa de atenção · Núcleo · Serviços de comunicação · Tarefas agendadas |
| System states | P | Running · Starting · Stopped · Failed · Not answering · Paused · Not checked | Rodando · Iniciando · Parado · Falhou · Sem resposta · Pausado · Sem verificação |
| System buttons | P | Start · Stop · Link… · Logs · Apply · Back to default | Iniciar · Parar · Vincular… · Logs · Aplicar · Voltar ao padrão |
| Routing switch | P | Handle requests: On/Off. Off pauses request handling; the module keeps running. | Processar solicitações: Ligado/Desligado. Desligado pausa o processamento; o módulo continua rodando. |
| Job names | P | Refresh memes (meme.refresh) · Post memes (meme.dispatch) | Atualizar memes · Postar memes |
| Interval | P | Every [6] [hours] | A cada [6] [horas] |
| Setting values | P | Yes / No · Open / Closed | Sim / Não · Abertas / Fechadas |
| Modules footnote | P | Modules keep running after you close the desktop. Stopping keeps their data. | Os módulos continuam funcionando ao fechar o desktop. Parar preserva os dados. |
| Providers caveat | P | Discovery only: authentication, quota and inference are not tested. Listing a model does not select it. | Só descoberta: autenticação, cota e inferência não foram testadas. Listar um modelo não o seleciona. |
| Generic error | P | The reply was not what this screen expects. | A resposta não é a que esta tela espera. |

Rules for new copy: voice lines in first person only in Talk, Knowledge, Memory, one per view;
numbers and states never inside a voice line; no copy ever calls a screen a "card".

---

## 6. Components for ui-designer

Structure and states only; colour, type and spacing are yours. All keep the dead-channel
cyberpunk look the owner kept; neon still only means a state.

| Component | Anatomy | States and variants |
|---|---|---|
| **WorkspacePanel** | chamfered panel filling the work area; legend (breadcrumb); header; optional voice line; optional toolbar; body; optional aside; optional dock | loading, ready, empty, error, off, offline; aside open/closed; narrow (<1000 px) aside as overlay; reduced motion |
| **ViewTabs** | underline tabs, fixed views then separator then closable item tabs; overflow menu when many | default, hover, focus-visible, selected, item-closable (× has its own focus/hover), loading badge, max length with ellipsis + tooltip |
| **VoiceLine** | one line in Bodoni italic, optional, never interactive | shown, hidden after first interaction; two lengths (short/long wrap) |
| **Button** | primary (one per view), secondary, quiet/inline, danger, icon-only (refresh, ⋯) | default, hover, focus-visible, active, disabled (with reason tooltip), busy (cursor print), destructive-confirming |
| **Switch** | track + thumb + visible state word ("On"/"Off") + label | on, off, busy, disabled, unknown (status not read), focus-visible. Only for persistent states. |
| **SegmentedControl** | 2 to 4 options, optional counts ("Waiting 7") | selected, hover, focus-visible, disabled, roving tabindex |
| **StateWord** | dot/shape + word, for services, interfaces, sources, experiences | working, problem, off/paused, unchecked, in-progress, unknown; shape differs per tone, word always present |
| **DataRow / DataTable** | leading title, meta columns, trailing state, hover/focus actions (⋯) | default, hover, focus, selected, busy, error row, empty cell placeholder |
| **ContextMenu** | right-click, Menu key, Shift+F10, ⋯ button; items with icons optional | open, item hover/focus, disabled item, destructive item (native confirm follows), Esc closes, focus returns to row |
| **Drawer / Popover** | aside drawer (Add), upward popover (meme picker), log aside | opening, open, loading content, error, closing; focus trap for drawers; Esc |
| **Composer** | multiline input, context chip, attachment chip, identity label ("as Ningo" / "as you"), send | idle, typing, over-limit counter, sending, disabled (offline, with reason), error + retry, uncertain delivery (timeout) |
| **Transcript** | turns with author + time, system dividers, thinking row, error row, "Jump to latest" | empty, streaming-free (whole reply), long message, error turn, notice ("fresh conversation"), scroll anchored |
| **Chip** | context ("Discussing: …"), attachment, quiet status ("similarity: on") | default, removable (×), warning, disabled |
| **EmptyState** | voice line + plain next step + optional action | for each workspace; also "off" and "offline" variants with "Open System" |
| **InlineNotice / Banner** | plain sentence, optional action | ok, info, unchecked/warning, problem; aria-live polite vs alert |
| **FieldRow / SettingsGrid** | label | value | action on one shared grid; helper text; inline error | read, editing, reviewing, saving, error, read-only ("Fixed for this version") |
| **IntervalField** | "Every [n] [unit]" as one control; Apply only when edited | pristine, edited, invalid, busy, "custom" marker + Back to default |
| **MemeTile / MemeGrid** | thumbnail is the button; check badge when attached; title below; pager | loading, ready, failed preview, video, attached, focus-visible, disabled |
| **Pager / LoadMore** | "1–20 of 23", previous/next; "Show 8 more" | first, middle, last, loading, at limit |
| **SidebarItem** | label, dot + shape, optional count badge, key hint | selected, hover, focus-visible, problem count, offline mark, stale mark |
| **Wordmark** | existing ghost states, now a link to Talk | tuning, calm, unwell, lost, stale |

---

## 7. Phased plan (one PR each)

Order follows harm. Each PR keeps lazy mounting, `Transport`, the allow-list and native
confirmations; each updates the i18n catalogs in both locales (the parity tests in
`tests/ui/i18n.test.ts` must pass).

| PR | Change | Files (renderer unless noted) | Tests to touch | Backend |
|---|---|---|---|---|
| **0 docs** | After the owner answers section 8: record the decisions in `dokja_docs/desktop-app-plan.md` ("UX replan 2026-10-23"), fix the stale README (five workspaces, Talk first, Ctrl+1..5) | `dokja_docs/desktop-app-plan.md`, `dokja_interfaces/desktop/README.md` | none | none |
| **1 status + front door** | Move the single `ningo.status` read into a shell-level `StatusProvider`; Pulse, sidebar tones and System read it; `START = "assistant"`; Talk gets `services: ["chat_ai"]`; foot shows "offline" when the read fails; pause refresh during a chat turn | `app.tsx`, `pulse.tsx` (merge into new `status_context.tsx`), `cards/health.tsx` (drop own load/report), `cards/registry.ts`, `sidebar.tsx` | `tests/ui/app.test.tsx` (`:110`, pulse tests), `sidebar.test.tsx`, `health_card.test.tsx`; `tests/e2e/smoke.e2e.ts`: open System before reading `.services li`; `seen == ["ningo.status"]` still holds | none |
| **2 template** | `WorkspacePanel` (legend = breadcrumb, header, body scroll, aside, dock, states); fixed-height work area; remove `card-kind` repeat and per-card footers; header refresh icon; auto-load helper; wrap the five existing views unchanged | new `cards/workspace_panel.tsx`; `card_frame.tsx` (keep `Pending/Problem/OffNotice`), `styles.css` (ui-designer), each card's wrapper | all `tests/ui/*` that query `.card`, kind labels, footers | none |
| **3 controls + vocabulary** | `Switch`, `SegmentedControl`, `Button` variants, `StateWord`; `display_names.ts` (module/service/job/setting-value/action maps with raw fallback); glossary, capitalisation, pt neutral phrasing, remove "card"; drop IDs from labels | new `controls.tsx`, `display_names.ts`; `i18n/locales/*.json`; call sites | `i18n.test.ts`, `health_switches.test.tsx`, `runtime.test.tsx`, `settings.test.tsx` | none |
| **4 System** | Status view: Needs attention + one Parts table (join of `runtime.modules` and `ningo.status`), jobs with inline interval, logs/options in the aside; Settings view with one field grid and localised values; Providers as a lazy sub-view; add `runtime.providers` to the fake transport | `cards/health.tsx`, `runtime.tsx`, `settings.tsx`, `providers.tsx`, new `parts_model.ts`, `fake_transport.ts` | `runtime`, `settings`, `providers`, `link_panel`, `health_*` tests; `tests/e2e/smoke.e2e.ts` (its WhatsApp test queries `.interfaces button`) | none (flag B6) |
| **5 Talk** | Conversation provider above the workspaces; docked `Composer`; scrollable `Transcript`; voice opening line; chips fill; context chip + divider; "Discuss" in Readings navigates here (remove the inline chat); offline state; "Start a new conversation" into ⋯ | `assistant.tsx`, `assistant_conversation.tsx` (split into `conversation_context.tsx`, `transcript.tsx`, `composer.tsx`), `digest.tsx`, `sidebar.tsx` (expose `useWorkspace().open`), i18n | new `tests/ui/talk.test.tsx`; `digest_card.test.tsx` | B1, B2 flagged; works without them |
| **6 Knowledge** | Documents-first list auto-loaded; toolbar search; Add drawer (+ drop overlay); row context menu (Open, Classify…, Delete…); merged tab strip; Readings sub-view with a Sources strip; name unification | `knowledge_workspace.tsx`, `knowledge.tsx`, `knowledge_documents.tsx`, `knowledge_add.tsx`, `document_view.tsx`, `digest.tsx`, new `context_menu.tsx`, `drawer.tsx` | `knowledge_card`, `knowledge_add`, `knowledge_documents`, `document_view`, `digest_card`, `knowledge_classification` | B3, B4 flagged |
| **7 Memory** | Headline + track record strip, auto-loaded experiences with filters and counts, row sentences, aside detail, caveats under "More" | `memory.tsx`, `memory_experiences.tsx`, `experience_feedback.tsx`, i18n | `memory_card`, `memory_experiences` | none |
| **8 Messages** | Docked composer, collapsible channel rail, meme popover and tile redesign, single refresh model, "Load earlier" at top, identity labels | `communications.tsx`, `memes.tsx` | `communications`, `whatsapp_conversations`, `tests/e2e/communications.e2e.ts` | none |

PR 2 is the largest and most mechanical; if it is too big, split it as 2a (template and Memory and
Messages shells) and 2b (System, Knowledge and Talk shells). The ui-designer can start from section
6 as soon as PR 2's regions are agreed.

---

## 8. Decisions needed from the owner

Changes to recorded decisions (explicit, not silent):

1. **Status read moves to the shell.** Replan: "dots come only from the one `ningo.status` the
   System view reads". Proposed: the same single request, owned by a shell provider at start.
2. **Opening workspace changes** from System (code) to Talk.
3. **Panel**: keep the chamfered panel and legend (Phase C decision) but one per workspace, legend
   as breadcrumb. No reversal.
4. **Context menu** on document rows: Phase C said none until it has a real action. Classify… now
   exists (C2/C3 built), so it earns its place. Needs a go-ahead (B4: no per-row "pending
   classification" flag, so Classify… is always offered).
5. **Discussion of a reading moves from an inline chat in the list to the Talk workspace**, with
   the reading as context chip (still the existing conversation model, `reading_id`).
6. **Composer docking.** Replan diagram shows one composer at the bottom of every workspace.
   Proposed: dock in Talk (the full composer); in Messages the dock is the channel composer,
   labelled by identity, and never the "Ask Ningo" box (a stray press must not post to a channel);
   a global "Ask Ningo" dock on Knowledge/Memory/System is optional and can follow once documents
   can be passed as context (B2).

## 9. Open questions

1. Global "Ask Ningo" on every workspace, or only in Talk (recommended for now)?
2. Open on Talk even when the orchestrator is down (offline state plus a link to System), or fall
   back to System when the status read fails?
3. Rename the first sidebar entry to "Talk / Conversa" (the wordmark above already says Ningo), and
   keep "Knowledge / Memory" or rename to "Library / Notebook"?
4. First-person Ningo voice in the UI ("I've read...") acceptable? And Ningo's gender in pt
   (`pt.json:347` says "Ela", other strings say "o Ningo")? The pt copy in section 5 was written
   without an article on purpose, so the question stays open; the owner picks one and the
   catalogs follow.
5. Voice typeset in Bodoni italic only, everything else mono: yes?
6. Should the conversation survive a restart (needs B1), or is a fresh thread per launch right?
7. System as three sub-views (Status, Settings, Providers): confirm.
8. Alt+1..3 for sub-views and Ctrl+K for the composer: acceptable on the target platforms?

## 10. Backend gaps (flagged, not designed)

| # | Gap | Why it matters | Renderer-only today? |
|---|---|---|---|
| B1 | No chat history or conversation list; `conversation_id` is generated client-side and the reply is a whole text (no streaming); chat plan owns session and history | Thread across restarts, "only child" continuity | No. Interim: in-memory transcript. |
| B2 | Verify whether `message.created` accepts a knowledge document as context; today `chatPayload` carries only `reading_id` (digest item) (`main/actions.ts:124-130`) | Discuss a document; global ask with context | Readings: yes. Documents: no. |
| B3 | Search hits cannot open their document: the service returns `source_id` (`dokja_services/dokja_knowledge/service.py:390`) but the shell projection drops it (`main/actions.ts:222-241`), and there is no read-by-id (plan step-09 "per-document detail contract" pending) | Click a hit to read the document | Partly: add `sourceId` to the projection (shell); by-id read is backend |
| B4 | `knowledge.list` has no "has pending classification" flag, no sort or filter by kind/tag | Gate Classify… per row; library filters | No |
| B5 | Settings values show channel ids; names are not returned with them (verify whether `communications.channels` covers the webhook channels) | Readable channel settings | Maybe |
| B6 | Service `detail`/error strings and job outcomes are free English text | Localised, scannable failure reasons | Shown as sent; codes would fix it |
| B7 | Ningo's one-line take on a reading or document needs a text generator (plan: Risks) | Judgement attached to the library | No |
| B8 | `ningo.status` can take about 5 s when services are stopped and queues behind a chat turn | Safe focus refresh of the sidebar | Mitigated by "pause during a chat turn" and a stale mark |

Renderer-only and already possible: mapping action names, states and values through closed lists
(with raw fallback), dropping ids from labels, relative times instead of ISO strings, the Parts
table join, moving the status read to the shell.

---

## 11. Screenshots

All in `screenshots/`:
`01-system-viewport-pt.webp`, `02-system-full-pt.webp`, `03-ningo-empty-pt.webp`,
`04-ningo-conversation-pt.webp`, `05-knowledge-documents-pt.webp`, `06-knowledge-docs-loaded-pt.webp`,
`07-knowledge-sources-pt.webp`, `08-memory-pt.webp`, `09-memory-experiences-open-pt.webp`,
`10-messages-pt.webp`, `11-messages-memes-pt.webp`, `12-system-options-open-pt.webp`,
`13-system-providers-pt.webp` (shows the fake-data limitation), `14-document-tab-pt.webp`,
`15-system-en.webp`, `16-ningo-en.webp`, `17-knowledge-search-en.webp`.
