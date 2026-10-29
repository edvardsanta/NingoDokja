# Post links: UX

Date: 2026-10-29. Author: UX designer agent. Scope: `dokja_interfaces/desktop` renderer, plus the shell
changes it needs. The owner asked for the lightest front end that fits what exists.

Status: built in `knowledge_add.tsx`, `knowledge_post.tsx` and `knowledge_add_model.ts`. Where the
build differs from the first draft (the save, the folder picker, the folder button), this text says
what was built.

## 1. Placement (decision)

**A fourth mode, "Post" (pt "Publicação"), in the existing "Add to the Library" drawer**
(Library > Documents > the primary button beside the search). Mode strip becomes
Note | Address | File | Post. No new workspace, view, tab or route; the new controls are one
mode button, one field, and "Show post" / "Save to the Library".

Why: pasting a post link and keeping it is an act of adding to the Library, and the drawer already
gives the focus trap, Esc/close, the live region and the outcome line. At 30 rem it fits one column of media.

Rejected, one line each:
- **Auto-detect in Address mode:** an extra request on every address; `unsupported` becomes normal.
- **A Library view for posts:** a list, empty state, detail and delete flow nobody asked for.
- **Pasting into Talk:** conversation and filing blur, and the composer has no preview.

In Post mode the Kind, Reference, Tags and "More options" fields are hidden: author, caption,
date and link fill them (kind `post`, reference = the link). No Ningo voice line (Library already
has its one); everything here is plain.

```
+-- Add to the Library ------------------- x -+
| [Note] [Address] [File] [Post*]             |
| Post link                                   |
| [https://example.com/p/123_________]        |
| A link to one public post. ...              |
| [Show post]          (secondary once shown) |
| ------------------------------------------- |
| Example Author, 20 Oct 2026      Files: 2   |  <- heading, meta
| +- caption (bounded, scrolls, tab stop) -+  |
| [Save to the Library]           (primary)   |
| [ Image 1 of 2 ]                            |
| [ Video 2 of 2, with controls ]             |
+---------------------------------------------+
```

## 2. Flow

1. Add to the Library opens the drawer; focus lands on the first mode button, as today. Choose Post
   (the drawer opens on the mode last used in this run). Note/Address/File drafts stay mounted, hidden.
2. Paste the link, Enter or "Show post". Empty or non-http(s) text is refused at the field, nothing sent.
3. Reading: field, mode strip and Show are disabled; status "Reading the post...".
4. Refused: one message under the field, focus stays in the field (`aria-invalid`, `aria-describedby`).
5. Shown: heading, caption, Save, then media (loaded at once, stacked, no carousel). Focus stays in the
   field. A short status line, `post_shown`, is the only live region (`role="status"`); the post block
   sits outside it, so a screen reader never reads the whole caption. Tab: Show post, caption, Save,
   media. Save is before the media in the DOM so the keyboard path is short. Show turns secondary.
6. Editing the field discards the shown post, so Save always matches what is on screen.
7. Save: the shell opens the system's folder picker (section 5); choosing the folder is the
   confirmation. Closing the picker returns quietly to the shown post, focus back on Save, no message.
   Choosing: Save is disabled, "Saving the post...".
8. Saved: the post is replaced by one outcome line; the field is cleared and focused for the next
   link; the Library list and counts refresh (`onAdded`). When files were kept and the shell can open
   a folder, "Open folder" follows the outcome line and opens the folder the owner chose.
9. Closing the drawer or switching mode discards the shown post from the screen; mode buttons are
   disabled during a read or save. Temporary files stay until the app closes.

## 3. States

| State | What the owner sees | Controls |
|---|---|---|
| Empty | label, field, hint (`post_hint`); no result area | Show post (primary) |
| Invalid input | `post_need_link` or `add_bad_address` under the field, `role="alert"` | Show post |
| Loading | `post_reading` (`role="status"`, `aria-busy`), text stays under reduced motion | all disabled |
| `unsupported` | `post_refused_unsupported` | quiet button `post_use_address` switches to Address with the link carried over |
| `login_required` | `post_refused_login` | none; field editable |
| `unavailable` | `post_refused_unavailable` | none |
| `no_reader` | `post_refused_no_reader`, plain; no view is known to list readers, so no "Open System" | none |
| `failed` | `post_refused_failed`; Show post is the retry | Show post |
| Off, offline, timeout | existing `OffNotice` / `ErrorLine` | as today |
| Post shown | `post_shown` status; heading (author, else the link), date, `post_files`, caption, media | Save (primary), Show (secondary) |
| Shown, variants | text only: no media block; no caption: `post_no_caption`; files missing: `post_media_missing` (count from the shell) | Save keeps the rest |
| Saving | `post_saving`, Save disabled and `aria-busy` | none |
| Saved | `post_saved` + `post_files_copied` when `created` or `updated`; `add_unchanged` when neither | field focused; quiet `post_open_folder` when files were kept and the shell offers it |
| Folder not opened | `post_open_folder_failed` (error tone) next to the saved line | the same |
| Save failed | `post_save_failed` (nothing kept), then the usual `ErrorLine` for the cause; `add_timeout` when unknown (amber, not magenta) | Save stays, focused |

## 4. Copy (all plain, no voice)

"Reuse" rows already exist in both catalogs; use the id as is.

| id | en | pt |
|---|---|---|
| add_mode_post | Post | Publicação |
| post_field_link | Post link | Link da publicação |
| post_hint | A link to one public post. For an ordinary page, use Address. | O link de uma publicação pública. Para uma página comum, use Endereço. |
| post_show | Show post | Mostrar publicação |
| post_reading | Reading the post… | Lendo a publicação… |
| post_need_link | Paste a post link. | Cole o link de uma publicação. |
| add_bad_address (reuse) | That is not a web address. Use one that starts with http:// or https://. | Esse não é um endereço da web. Use um que comece com http:// ou https://. |
| post_refused_unsupported | No reader handles this link. If it is an ordinary page, add it as an address. | Nenhum leitor atende este link. Se for uma página comum, adicione-a como endereço. |
| post_use_address | Use as address | Usar como endereço |
| post_refused_login | This post needs a sign-in. Only public posts can be read. | Esta publicação exige login. Só é possível ler publicações públicas. |
| post_refused_unavailable | This post is private, was removed or was not found. | Esta publicação é privada, foi removida ou não foi encontrada. |
| post_refused_no_reader | No post reader is installed in the feeds service, so no link can be read yet. | Nenhum leitor de publicações está instalado no serviço de feeds, então ainda não dá para ler links. |
| post_refused_failed | The post could not be read. Try again. | Não foi possível ler a publicação. Tente de novo. |
| post_shown | Post shown: {title}. Files: {count}. | Publicação exibida: {title}. Arquivos: {count}. |
| post_files | Files: {count} | Arquivos: {count} |
| post_caption_label | Caption | Legenda |
| post_no_caption | This post has no caption. | Esta publicação não tem legenda. |
| post_image_n | Image {n} of {total} | Imagem {n} de {total} |
| post_video_n | Video {n} of {total} | Vídeo {n} de {total} |
| post_media_error | This file could not be shown. | Não foi possível mostrar este arquivo. |
| post_media_missing | Some files could not be downloaded ({count}). Saving keeps the others. Show the post again to retry. | Alguns arquivos não puderam ser baixados ({count}). Ao guardar, só os outros são mantidos. Mostre a publicação de novo para tentar outra vez. |
| reading_save (reuse) | Save to the Library | Guardar na Biblioteca |
| reading_save_named (reuse) | Save to the Library: {title} | Guardar na Biblioteca: {title} |
| post_saving | Saving the post… | Guardando a publicação… |
| post_saved | Saved to the Library: {title}. | Guardada na Biblioteca: {title}. |
| post_files_copied | Files copied to this computer: {count}. | Arquivos copiados para este computador: {count}. |
| add_unchanged (reuse) | Already in the Library. | Já está na Biblioteca. |
| post_open_folder | Open folder | Abrir pasta |
| post_open_folder_failed | The folder could not be opened. | Não foi possível abrir a pasta. |
| post_save_failed | The post could not be saved. Nothing was kept. | Não foi possível guardar a publicação. Nada foi mantido. |
| add_timeout (reuse) | (existing) No answer in time. The document may already be stored… | (existing) Sem resposta a tempo. O documento pode já estar guardado… |

A definite failure keeps nothing (the shell rolls back, see section 7); an unknown outcome is `add_timeout`.

## 5. Native folder picker (main process, `src/main/main.ts`)

Asked by the shell for the post save only, never for notes, addresses or files. It is the system's own
folder window, and **choosing the folder is the confirmation**: there is no separate yes/no box.
Closing it without a choice saves nothing and reaches the screen as the error code `cancelled`, which
the screen answers with silence. The files go into a folder named by the post's id inside the chosen
one; the Library entry is written from the shell's own record of the read, never from text the page
sends. "Open folder" opens the folder the owner chose, for a post saved in this run, on a real press.
Say "Library"/"Biblioteca", not "research base". The window's words are in the catalogs
(`post_pick_*`) and belong to the shell.

| id | en | pt |
|---|---|---|
| post_pick_title | Choose where to save this post | Escolha onde guardar esta publicação |
| post_pick_button | Save here | Guardar aqui |

## 6. Notes for the ui-designer

- Reuse `Drawer`; the mode strip stays the existing segmented control, never styled as an on/off switch.
- One primary at a time: Show post until a post is shown, then Save to the Library; Show drops to secondary.
- Post block: heading in the mono face (not the voice face), meta in `--ink-dim`, caption in the
  `.reading-preview-text` pattern (bounded height, scrolls, `tabIndex=0`, `aria-label` = `post_caption_label`).
- Media: one column, `max-width: 100%`, `max-height` about 60vh, `object-fit: contain`. Images get
  `alt` = `post_image_n`. Video: `controls`, `preload="metadata"`, never
  `autoplay` or `loop`, `aria-label` = `post_video_n`; a `poster` only if `PostMedia` gains a `poster?`. A failed item shows `post_media_error`.
- Tone: `unsupported`, `login_required`, `unavailable` are expected outcomes (neutral notice);
  `failed`, `no_reader` and save failures use the error tone. Magenta stays reserved for destructive actions.
- Reduced motion: no print-in of the result; the "Reading the post…" text must stay (the cursor stops).
  An animated image cannot be paused in CSS: show it as is for now.
- The date is absolute in the owner's locale (not "ago"); an unknown author or date is omitted.

## 7. Contract, gaps and open questions

- **Read:** `digest.link.read` (gesture-gated, no window), replies as `PostReply` in
  `src/shared/posts.ts`; off or a failed request come as for any action.
- **Save:** there is no separate save action. The screen sends `knowledge.ingest` with
  `{ mode: "post", post: documentId }` and only the shell's id; the shell asks for the folder, copies
  the files, calls the Library itself and rolls the copy back when the Library refuses. The reply is the
  usual `IngestResult`: `created` or `updated` -> `post_saved`; neither -> `add_unchanged`. There is no
  `PostSaved` type.
- **Folder:** `Transport.openSavedPost?(documentId)` is optional: the button is drawn only where the
  transport has it (the shell and the browser preview, not a plain test transport).
- **Contract:** `PostMedia` has no thumbnail; add `poster?` only if video posters are wanted.
- **Media:** shown through the `ningo-post` scheme, which the CSP allows (`img-src`, `media-src`); the
  page never gets a file path. The temporary folder lifecycle belongs to the shell.
- **Built:** the strip and the last-used way are held by `knowledge.tsx` (`startMode`, `onMode`);
  `knowledge_post.tsx` is the pane; the Drawer's focus trap now counts a video with controls, so Tab
  reaches the clip and wraps only after it.
- **Browser preview:** the fake transport answers `digest.link.read` by the path of the link
  (`/login`, `/private`, `/page`, `/broken`, `/reader` are refused; `/text` has no files; any other is
  a post with two pictures) and saves by id.
- **Owner:** (1) pt "Link da publicação" or "Endereço da publicação" (the catalog says "endereço"
  elsewhere)? Built as "Link da publicação". (2) Decided: the saved line offers "Open folder".
