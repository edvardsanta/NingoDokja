import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { AddToBase } from "../../src/renderer/cards/knowledge_add.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { PostRefusal, PostView } from "../../src/shared/posts.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, OFF, deferred, failure, ok, routedTransport } from "./support.js";

const LINK = "https://example.com/p/123";
const ID = "post-0123456789abcdef";

const POST: PostView = {
  documentId: ID,
  url: LINK,
  author: "Example Author",
  caption: "A caption worth reading.",
  published: "2026-10-20T12:00:00Z",
  media: [
    { kind: "image", src: `ningo-post://${ID}/0` },
    { kind: "video", src: `ningo-post://${ID}/1` },
  ],
  missing: 0,
};
const posted = (overrides: Partial<PostView> = {}) => ok({ status: "ok", post: { ...POST, ...overrides } });
const refused = (refused: PostRefusal) => ok({ status: "refused", refused });

const CREATED = { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 1, degraded: false, reason: "" };
const UNCHANGED = { ...CREATED, created: 0, unchanged: 1, chunks: 0 };

function renderPost(transport: Transport, locale: Locale = "en", onAdded?: () => void) {
  return render(
    <I18nProvider locale={locale}>
      <AddToBase transport={transport} onAdded={onAdded} startMode="post" />
    </I18nProvider>,
  );
}

const field = () => screen.getByLabelText(/^(Post link|Link da publicação)$/) as HTMLInputElement;
const show = () => screen.getByRole("button", { name: /^(Show post|Mostrar publicação)$/ });
const keep = () => screen.getByRole("button", { name: /^Save to the Library/ });
const type = (value: string) => fireEvent.change(field(), { target: { value } });
const reads = (calls: Array<{ type: string }>) => calls.filter((call) => call.type === "digest.link.read");
const saves = (calls: Array<{ type: string }>) => calls.filter((call) => call.type === "knowledge.ingest");

// Types a link and presses Show post, then waits for the post to be on screen.
async function open(transport: Transport, locale: Locale = "en") {
  const view = renderPost(transport, locale);
  type(LINK);
  fireEvent.click(show());
  await screen.findByRole("heading", { name: "Example Author" });
  return view;
}

describe("the Post way of adding", () => {
  it("is the fourth way, and shows its own field instead of the form", () => {
    render(
      <I18nProvider locale="en">
        <AddToBase transport={routedTransport({}).transport} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Post" }));

    expect(screen.getByRole("button", { name: "Post" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Note" }).getAttribute("aria-pressed")).toBe("false");
    screen.getByLabelText("Post link");
    screen.getByText("A link to one public post. For an ordinary page, use Address.");
    for (const hidden of ["Title", "Text", "Kind", "Where it came from (optional)", "Tags (optional, separated by commas)"]) {
      expect(screen.queryByLabelText(hidden), hidden).toBeNull();
    }
    expect(screen.queryByText("More options")).toBeNull();
  });

  it("keeps what was typed in the other ways while the post is being looked at", () => {
    renderPost(routedTransport({}).transport);
    fireEvent.click(screen.getByRole("button", { name: "Note" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "A thought" } });
    fireEvent.click(screen.getByRole("button", { name: "Post" }));
    fireEvent.click(screen.getByRole("button", { name: "Note" }));

    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("A thought");
  });

  it("reads a link in the press itself, then shows the post with its files, Save before them", async () => {
    const slow = deferred<TransportResult>();
    const { transport, calls } = routedTransport({ "digest.link.read": () => slow.promise });
    renderPost(transport);
    type(`  ${LINK}  `);

    fireEvent.click(show());
    // the request leaves in the press, with nothing awaited first: the shell reads a link only on a real press
    expect(calls).toEqual([{ type: "digest.link.read", payload: { url: LINK }, options: { timeoutMs: 120_000 } }]);
    const status = screen.getByRole("status");
    expect(status.textContent).toBe("Reading the post…");
    expect(status.getAttribute("aria-busy")).toBe("true");
    expect(field().readOnly).toBe(true);
    expect((show() as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Note" }) as HTMLButtonElement).disabled).toBe(true);
    expect(document.activeElement).toBe(field());
    fireEvent.submit(field().closest("form") as HTMLFormElement);
    expect(reads(calls)).toHaveLength(1);

    await act(async () => {
      slow.resolve(posted());
    });

    await screen.findByRole("heading", { name: "Example Author" });
    expect(status.textContent).toBe("Post shown: Example Author. Files: 2.");
    expect(status.getAttribute("aria-busy")).toBe("false");
    expect(field().readOnly).toBe(false);
    // the live region says what happened; the post itself is never read out through it
    const caption = screen.getByLabelText("Caption");
    expect(caption.textContent).toBe("A caption worth reading.");
    expect(caption.getAttribute("tabindex")).toBe("0");
    expect(status.contains(caption)).toBe(false);
    expect(screen.getByText(/Oct 2[01], 2026, Files: 2/)).toBeTruthy();
    expect(show().className).not.toContain("primary");
    expect(keep().className).toContain("primary");
    expect(keep().getAttribute("aria-label")).toBe("Save to the Library: Example Author");
    expect(document.activeElement).toBe(field());

    const picture = screen.getByAltText("Image 1 of 2") as HTMLImageElement;
    expect(picture.getAttribute("src")).toBe(`ningo-post://${ID}/0`);
    const clip = screen.getByLabelText("Video 2 of 2") as HTMLVideoElement;
    expect(clip.getAttribute("src")).toBe(`ningo-post://${ID}/1`);
    expect(clip.controls).toBe(true);
    expect(clip.autoplay).toBe(false);
    expect(clip.loop).toBe(false);
    expect(clip.getAttribute("preload")).toBe("metadata");
    // Save is reached before the files: keyboard order is the order in the page
    expect(keep().compareDocumentPosition(picture) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(keep().compareDocumentPosition(clip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("refuses an empty or non-web link at the field, sending nothing", () => {
    const { transport, calls } = routedTransport({ "digest.link.read": posted() });
    renderPost(transport);

    fireEvent.click(show());
    let alert = screen.getByRole("alert");
    expect(alert.textContent).toBe("Paste a post link.");
    expect(field().getAttribute("aria-invalid")).toBe("true");
    expect(field().getAttribute("aria-describedby")?.split(" ")).toContain(alert.parentElement?.id);
    expect(document.activeElement).toBe(field());

    type("plugin:something");
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(show());
    alert = screen.getByRole("alert");
    expect(alert.textContent).toBe("That is not a web address. Use one that starts with http:// or https://.");
    expect(reads(calls)).toHaveLength(0);
  });

  it.each([
    ["unsupported", "No reader handles this link. If it is an ordinary page, add it as an address.", "off"],
    ["login_required", "This post needs a sign-in. Only public posts can be read.", "off"],
    ["unavailable", "This post is private, was removed or was not found.", "off"],
    ["no_reader", "No post reader is installed in the feeds service, so no link can be read yet.", "problem"],
    ["failed", "The post could not be read. Try again.", "problem"],
  ] as const)("says in words why a %s link is not shown, under the field", async (reason, words, tone) => {
    const { transport } = routedTransport({ "digest.link.read": refused(reason) });
    renderPost(transport);
    type(LINK);
    fireEvent.click(show());

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(words);
    expect(alert.getAttribute("data-tone")).toBe(tone);
    expect(field().getAttribute("aria-invalid")).toBe("true");
    expect(field().getAttribute("aria-describedby")?.split(" ")).toContain(alert.parentElement?.id);
    expect(screen.queryByRole("heading", { name: "Example Author" })).toBeNull();
    // focus stays where the link is, and the field can be edited again
    expect(document.activeElement).toBe(field());
    expect(field().readOnly).toBe(false);
    expect((show() as HTMLButtonElement).disabled).toBe(false);
    expect(show().className).toContain("primary");
    // only a link nobody reads offers the way around
    expect(screen.queryByRole("button", { name: "Use as address" }) !== null).toBe(reason === "unsupported");
  });

  it("carries the link of an unsupported page to the Address way", async () => {
    const { transport } = routedTransport({ "digest.link.read": refused("unsupported") });
    renderPost(transport);
    type(` ${LINK} `);
    fireEvent.click(show());

    fireEvent.click(await screen.findByRole("button", { name: "Use as address" }));

    expect(screen.getByRole("button", { name: "Address" }).getAttribute("aria-pressed")).toBe("true");
    const address = screen.getByLabelText("Web address") as HTMLInputElement;
    expect(address.value).toBe(LINK);
    expect(document.activeElement).toBe(address);
    expect(screen.queryByLabelText("Post link")).toBeNull();
  });

  it("says a switched-off service is off, and explains a failed read like any other", async () => {
    const view = renderPost(routedTransport({ "digest.link.read": ok(OFF) }).transport);
    type(LINK);
    fireEvent.click(show());
    await screen.findByText("Switched off.");
    view.unmount();

    renderPost(routedTransport({ "digest.link.read": [failure("timeout"), failure("unavailable"), ok({ status: "other" })] }).transport);
    type(LINK);
    fireEvent.click(show());
    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator took too long to answer.");
    // the link is not at fault when the orchestrator is not answering
    expect(field().getAttribute("aria-invalid")).toBeNull();
    fireEvent.click(show());
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("The orchestrator is not answering. Is it running?"));
    fireEvent.click(show());
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("The reply was not what this card expects."));
  });

  it("shows a post with no files or no caption plainly, and says what could not be downloaded", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted({ caption: "  ", media: [], missing: 2 }) });
    await open(transport);

    screen.getByText("This post has no caption.");
    screen.getByText("Some files could not be downloaded (2). Saving keeps the others. Show the post again to retry.");
    expect(document.querySelector(".add-post-media")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("Post shown: Example Author. Files: 0.");
  });

  it("names a post by its link when nobody is named", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted({ author: "" }) });
    renderPost(transport);
    type(LINK);
    fireEvent.click(show());

    await screen.findByRole("heading", { name: LINK });
    expect(keep().getAttribute("aria-label")).toBe(`Save to the Library: ${LINK}`);
  });

  it("says which file could not be shown", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted() });
    await open(transport);

    fireEvent.error(screen.getByAltText("Image 1 of 2"));
    screen.getByText("Image 1 of 2. This file could not be shown.");
    fireEvent.error(screen.getByLabelText("Video 2 of 2"));
    screen.getByText("Video 2 of 2. This file could not be shown.");
  });

  it("discards the post as soon as the link is edited, so Save always matches what is on screen", async () => {
    const { transport, calls } = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": ok(CREATED) });
    await open(transport);

    type(`${LINK}4`);

    expect(screen.queryByRole("heading", { name: "Example Author" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Save to the Library/ })).toBeNull();
    expect(screen.queryByAltText("Image 1 of 2")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("");
    expect(show().className).toContain("primary");
    expect(saves(calls)).toHaveLength(0);
  });

  it("saves the post by its id, then shows one line, clears the field and tells the Library", async () => {
    const onAdded = vi.fn();
    const slow = deferred<TransportResult>();
    const { transport, calls } = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": () => slow.promise });
    render(
      <I18nProvider locale="en">
        <AddToBase transport={transport} onAdded={onAdded} startMode="post" />
      </I18nProvider>,
    );
    type(LINK);
    fireEvent.click(show());
    await screen.findByRole("heading", { name: "Example Author" });

    fireEvent.click(keep());
    // the shell asks where to keep it; the request leaves in the press, and nothing is sent twice
    expect(saves(calls)).toEqual([{ type: "knowledge.ingest", payload: { mode: "post", post: ID }, options: { timeoutMs: 120_000 } }]);
    expect(screen.getByRole("status").textContent).toBe("Saving the post…");
    expect((keep() as HTMLButtonElement).disabled).toBe(true);
    expect(keep().getAttribute("aria-busy")).toBe("true");
    expect((screen.getByRole("button", { name: "Note" }) as HTMLButtonElement).disabled).toBe(true);
    expect(document.activeElement).toBe(field());
    fireEvent.click(keep());
    expect(saves(calls)).toHaveLength(1);

    await act(async () => {
      slow.resolve(ok(CREATED));
    });

    await screen.findByText("Saved to the Library: Example Author.");
    screen.getByText("Files copied to this computer: 2.");
    expect(onAdded).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("heading", { name: "Example Author" })).toBeNull();
    expect(screen.queryByAltText("Image 1 of 2")).toBeNull();
    expect(field().value).toBe("");
    expect(document.activeElement).toBe(field());
    expect(field().readOnly).toBe(false);
    expect((screen.getByRole("button", { name: "Note" }) as HTMLButtonElement).disabled).toBe(false);
    expect(show().className).toContain("primary");
  });

  it("says a post that was already there is already there, and does not tell the Library", async () => {
    const onAdded = vi.fn();
    const { transport } = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": ok(UNCHANGED) });
    render(
      <I18nProvider locale="en">
        <AddToBase transport={transport} onAdded={onAdded} startMode="post" />
      </I18nProvider>,
    );
    type(LINK);
    fireEvent.click(show());
    await screen.findByRole("heading", { name: "Example Author" });
    fireEvent.click(keep());

    await screen.findByText("Already in the Library.");
    expect(screen.queryByText(/Files copied/)).toBeNull();
    expect(onAdded).not.toHaveBeenCalled();
  });

  it("says a post saved over an earlier version is saved", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": ok({ ...CREATED, created: 0, updated: 1 }) });
    await open(transport);
    fireEvent.click(keep());

    await screen.findByText("Saved to the Library: Example Author.");
  });

  it("stays quiet when the owner cancels the shell's question, and goes back to Save", async () => {
    const { transport, calls } = routedTransport({
      "digest.link.read": posted(),
      "knowledge.ingest": [failure("cancelled", "the question was declined"), ok(CREATED)],
    });
    await open(transport);

    fireEvent.click(keep());

    await waitFor(() => expect((keep() as HTMLButtonElement).disabled).toBe(false));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("Cancelled.")).toBeNull();
    screen.getByRole("heading", { name: "Example Author" });
    expect(screen.getByRole("status").textContent).toBe("Post shown: Example Author. Files: 2.");
    expect(document.activeElement).toBe(keep());

    fireEvent.click(keep());
    await screen.findByText("Saved to the Library: Example Author.");
    expect(saves(calls)).toHaveLength(2);
  });

  it("says nothing was kept when the save fails, and a timeout may still have worked", async () => {
    const { transport } = routedTransport({
      "digest.link.read": posted(),
      "knowledge.ingest": [failure("orchestrator", "the knowledge service refused"), failure("timeout"), ok(CREATED)],
    });
    await open(transport);

    fireEvent.click(keep());
    await screen.findByText("The post could not be saved. Nothing was kept.");
    screen.getByText("The orchestrator answered with an error: the knowledge service refused");
    // the post stays, with Save to try again
    screen.getByRole("heading", { name: "Example Author" });
    expect(document.activeElement).toBe(keep());

    fireEvent.click(keep());
    await screen.findByText("No answer in time. It may already be stored. Reload the document list before trying again.");
    expect(screen.queryByText("The post could not be saved. Nothing was kept.")).toBeNull();

    fireEvent.click(keep());
    await screen.findByText("Saved to the Library: Example Author.");
  });

  it("says a switched-off Library is switched off, and keeps the post", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": ok(OFF) });
    await open(transport);
    fireEvent.click(keep());

    await screen.findByText("Switched off.");
    screen.getByRole("heading", { name: "Example Author" });
  });

  describe("the folder of a saved post", () => {
    const withFolder = (open: Transport["openSavedPost"], routes = { "digest.link.read": posted(), "knowledge.ingest": ok(CREATED) }) => {
      const { transport } = routedTransport(routes);
      return { ...transport, openSavedPost: open } satisfies Transport;
    };

    it("is offered once the post is saved, and opens in the press", async () => {
      const opening = vi.fn(async () => ok({ opened: true }));
      await open(withFolder(opening));
      expect(screen.queryByRole("button", { name: "Open folder" })).toBeNull();
      fireEvent.click(keep());

      const button = await screen.findByRole("button", { name: "Open folder" });
      fireEvent.click(button);
      expect(opening).toHaveBeenCalledWith(ID);
      await act(async () => {});
      expect(screen.queryByText("The folder could not be opened.")).toBeNull();
    });

    it("says when the folder could not be opened, next to the saved line", async () => {
      const opening = vi.fn(async () => failure("unavailable", "the folder could not be opened"));
      await open(withFolder(opening));
      fireEvent.click(keep());

      fireEvent.click(await screen.findByRole("button", { name: "Open folder" }));

      expect((await screen.findByRole("alert")).textContent).toBe("The folder could not be opened.");
      screen.getByText("Saved to the Library: Example Author.");
    });

    it("is not offered where there is no folder to open, or no file was kept", async () => {
      const plain = routedTransport({ "digest.link.read": posted(), "knowledge.ingest": ok(CREATED) });
      const view = await open(plain.transport);
      fireEvent.click(keep());
      await screen.findByText("Saved to the Library: Example Author.");
      expect(screen.queryByRole("button", { name: "Open folder" })).toBeNull();
      view.unmount();

      await open(withFolder(vi.fn(), { "digest.link.read": posted({ media: [] }), "knowledge.ingest": ok(CREATED) }));
      fireEvent.click(keep());
      await screen.findByText("Saved to the Library: Example Author.");
      expect(screen.queryByRole("button", { name: "Open folder" })).toBeNull();
    });
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport({ "digest.link.read": posted({ missing: 1 }), "knowledge.ingest": ok(CREATED) });
    renderPost(transport, "pt");

    fireEvent.click(show());
    expect(screen.getByRole("alert").textContent).toBe("Cole o link de uma publicação.");
    type(LINK);
    fireEvent.click(show());
    await screen.findByRole("heading", { name: "Example Author" });
    expect(screen.getByRole("status").textContent).toBe("Publicação exibida: Example Author. Arquivos: 2.");
    screen.getByLabelText("Legenda");
    screen.getByAltText("Imagem 1 de 2");
    screen.getByLabelText("Vídeo 2 de 2");
    screen.getByText("Alguns arquivos não puderam ser baixados (1). Ao guardar, só os outros são mantidos. Mostre a publicação de novo para tentar outra vez.");
    expect(screen.getByRole("button", { name: "Guardar na Biblioteca: Example Author" }).textContent).toBe("Guardar na Biblioteca");

    fireEvent.click(screen.getByRole("button", { name: /^Guardar na Biblioteca/ }));
    await screen.findByText("Guardada na Biblioteca: Example Author.");
    screen.getByText("Arquivos copiados para este computador: 2.");
  });
});

describe("the Post way in the Library's drawer", () => {
  const NO_DOCUMENTS = ok({ documents: [], total: 0, offset: 0 });

  // the drawer belongs to the Library's workspace, not to its Documents view
  function renderCard(transport: Transport) {
    return render(
      <I18nProvider locale="en">
        <KnowledgeWorkspace transport={transport} />
      </I18nProvider>,
    );
  }

  const routes = {
    "knowledge.list": NO_DOCUMENTS,
    "knowledge.status": ok(KNOWLEDGE_STATUS),
    "digest.link.read": posted(),
    "knowledge.ingest": ok(CREATED),
  };

  it("opens on the way last used, and refreshes the Library after a post is saved", async () => {
    const { transport, calls } = routedTransport(routes);
    renderCard(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Add to the Library" }));
    expect(screen.getByRole("button", { name: "Note" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Post" }));

    fireEvent.click(screen.getByRole("button", { name: "Close Add to the Library" }));
    fireEvent.click(await screen.findByRole("button", { name: "Add to the Library" }));
    expect(screen.getByRole("button", { name: "Post" }).getAttribute("aria-pressed")).toBe("true");

    const before = calls.filter((call) => call.type === "knowledge.status").length;
    type(LINK);
    fireEvent.click(show());
    await screen.findByRole("heading", { name: "Example Author" });
    fireEvent.click(keep());
    await screen.findByText("Saved to the Library: Example Author.");
    await waitFor(() => expect(calls.filter((call) => call.type === "knowledge.status").length).toBe(before + 1));
  });

  it("does not let an earlier drop take the way last used back when the drawer opens again", async () => {
    const { transport } = routedTransport(routes);
    renderCard(transport);
    const counts = await screen.findByText("6 documents in 52 passages");
    fireEvent.drop(counts, { dataTransfer: { types: ["Files"], files: [new File(["hello"], "far.md")] } });
    await screen.findByText("far.md, 1 KB");
    fireEvent.click(screen.getByRole("button", { name: "Post" }));

    fireEvent.click(screen.getByRole("button", { name: "Close Add to the Library" }));
    fireEvent.click(await screen.findByRole("button", { name: "Add to the Library" }));

    expect(screen.getByRole("button", { name: "Post" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "File" }));
    expect(screen.queryByText("far.md, 1 KB")).toBeNull();
  });

  it("keeps Tab inside the drawer, through the post's video controls", async () => {
    const { transport } = routedTransport(routes);
    renderCard(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Add to the Library" }));
    fireEvent.click(screen.getByRole("button", { name: "Post" }));
    type(LINK);
    fireEvent.click(show());
    await screen.findByRole("heading", { name: "Example Author" });

    const dialog = screen.getByRole("dialog");
    // Save is not the last stop while a video with controls follows it: the browser takes the Tab
    // to the video, and the drawer wraps only after it
    keep().focus();
    expect(fireEvent.keyDown(keep(), { key: "Tab" })).toBe(true);
    expect(within(dialog).getByLabelText("Video 2 of 2")).toBeTruthy();
  });
});
