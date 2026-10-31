# Desktop component catalog

Step `09.7`. The building blocks of the desktop's screens, what each one owns, and
how far consumers have moved to them. Existing primitives are extended before a new
one is added, no component dependency is introduced, and the rendered HTML of a
migrated control does not change, so the visual system stays as it is.

## Primitives

| Primitive | Where | API | Owns |
| --- | --- | --- | --- |
| `WorkspacePanel` | `cards/workspace_panel.tsx` | `trail`, `views`, `actions`, `dock`, `fill` | The frame of a workspace and its header/dock slots |
| `CardFrame` | `cards/card_frame.tsx` | `title`, `state`, `onReload`, `supplement`, render child | Loading, failed and off states of a section |
| `Drawer` | `cards/drawer.tsx` | `title`, `onClose` | Modal side panel: focus entry, Tab containment, Escape, focus return, portal outside the size container |
| View tabs | `cards/view_tabs.tsx` | `useViewTabs` | Inner tabs and their keyboard |
| Row menu | `cards/context_menu.tsx` | `useRowMenu`, `MenuRow` | Row actions and their keyboard |
| `Button` | `cards/ui.tsx` | `variant`, `size`, `type`, `extra` | The look by name, and `type="button"` unless asked otherwise |
| `Notice` | `cards/ui.tsx` | `announce`, `boxed`, `tone`, `as`, `extra` | Whether feedback is announced, and how it is drawn |

## States

`Notice` covers these with one rule: announce what the person should hear when it
changes, and never streaming text.

| State | Use | Announce |
| --- | --- | --- |
| Loading, pending, queued, saved | `<Notice announce="polite">`, or `Pending` | polite |
| Failed | `<Notice announce="assertive">`, or `Problem` for a transport error | assertive |
| Unavailable, stale, unsupported | `<Notice boxed tone="problem">` or `tone="off"`, with its action beside it | only if it just changed |
| Empty, ready | Plain text or the content itself | none |

## Migration

| Vertical | State |
| --- | --- |
| Talk (`assistant`, `assistant_conversation`, `conversation_list`, `talk_model`) and `card_frame` | Migrated |
| Settings, Library, Notebook, Messages, System, Realtime | Not migrated; move each in its own batch, with its own tests |

`button.primary`, `.quiet`, `.compact` and `.note` remain the styling contract, so
migrated and unmigrated screens look the same while this happens. To roll a vertical
back, restore its class strings; nothing else depends on the primitives.

## Still open in `09.7`

Other verticals; linked inline errors with a focusable summary for multi-field
submits outside Settings; long-content and narrow-panel checks in the real app, which
belong with `09.6-b`. CLI and TUI keep their own presentation and share only the
state vocabulary.
