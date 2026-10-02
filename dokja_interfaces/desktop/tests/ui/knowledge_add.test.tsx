import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AddToBase } from "../../src/renderer/cards/knowledge_add.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { INGEST_LIMITS } from "../../src/shared/ingest.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { OFF, deferred, failure, ok, routedTransport } from "./support.js";

const CREATED = { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 2, degraded: false, reason: "" };

function renderForm(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <AddToBase transport={transport} />
    </I18nProvider>,
  );
}

const sent = (calls: Array<{ type: string; payload?: Record<string, unknown>; options?: { timeoutMs?: number } }>) =>
  calls.filter((call) => call.type === "knowledge.ingest");

const type = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

const mode = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const send = () => fireEvent.click(screen.getByRole("button", { name: "Add" }));

const chooseFile = (file: File) => {
  const input = screen.getByLabelText("File") as HTMLInputElement;
  fireEvent.change(input, { target: { files: [file] } });
};

describe("adding to the research base", () => {
  it("starts on a note, and the three kinds can be chosen", () => {
    renderForm(routedTransport({}).transport);

    expect(screen.getByRole("button", { name: "Note" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Address" }).getAttribute("aria-pressed")).toBe("false");
    screen.getByLabelText("Title");
    screen.getByLabelText("Text");
    mode("Address");
    screen.getByLabelText("Web address");
    expect(screen.queryByLabelText("Text")).toBeNull();
    mode("File");
    screen.getByLabelText("File");
  });

  it("sends the kind, the reference and the id that were chosen, and keeps the kind for the next one", async () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);
    expect((screen.getByLabelText("Kind") as HTMLInputElement).placeholder).toBe("note");

    type("Title", "A thought");
    type("Text", "Something to keep.");
    type("Kind", "idea");
    type("Where it came from (optional)", " Book, p. 12 ");
    fireEvent.click(screen.getByText("More options"));
    type("Id (optional)", "my:thought-1");
    send();

    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)[0]?.payload).toEqual({
      mode: "note", title: "A thought", body: "Something to keep.", kind: "idea", reference: "Book, p. 12", id: "my:thought-1",
    });
    await waitFor(() => expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe(""));
    expect((screen.getByLabelText("Where it came from (optional)") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Id (optional)") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Kind") as HTMLInputElement).value).toBe("idea");
  });

  it("offers kinds to pick from, shows the default of each way of adding, and takes any other word", () => {
    const view = renderForm(routedTransport({}).transport);

    const options = [...view.container.querySelectorAll("datalist option")].map((option) => option.getAttribute("value"));
    expect(options).toEqual(["note", "article", "book", "paper", "document", "list"]);
    mode("Address");
    expect((screen.getByLabelText("Kind") as HTMLInputElement).placeholder).toBe("article");
    mode("File");
    expect((screen.getByLabelText("Kind") as HTMLInputElement).placeholder).toBe("document");
  });

  it("says when a kind, a reference or an id is not allowed, before sending", () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);
    type("Title", "T");
    type("Text", "text");

    type("Kind", "Two Words");
    send();
    expect(screen.getByRole("alert").textContent).toBe("A kind is one lowercase word, such as note, article or book.");
    type("Kind", "idea");
    fireEvent.click(screen.getByText("More options"));
    type("Id (optional)", "has space");
    send();
    expect(screen.getByRole("alert").textContent).toBe("An id uses letters, digits and . _ : / # @ - (up to 200 characters).");
    expect(sent(calls)).toHaveLength(0);
  });

  it("warns that an id that already exists replaces that document", () => {
    renderForm(routedTransport({}).transport);
    fireEvent.click(screen.getByText("More options"));

    screen.getByText("Leave it empty and the id is made from what you add. Giving an id that already exists replaces that document.");
  });

  it("sends a note as the shell expects, then clears the form and says what happened", async () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);

    type("Title", "  A thought ");
    type("Text", "Something to keep.");
    type("Tags (optional, separated by commas)", "ethics, kant");
    send();

    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)).toEqual([
      {
        type: "knowledge.ingest",
        payload: { mode: "note", title: "A thought", body: "Something to keep.", tags: ["ethics", "kant"] },
        options: { timeoutMs: 120_000 },
      },
    ]);
    await waitFor(() => expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe(""));
    expect((screen.getByLabelText("Text") as HTMLTextAreaElement).value).toBe("");
  });

  it("says what is missing before anything is sent", () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);

    send();
    expect(screen.getByRole("alert").textContent).toBe("A note needs a title.");
    type("Title", "T");
    send();
    expect(screen.getByRole("alert").textContent).toBe("Write the text of the note.");
    type("Text", "x".repeat(INGEST_LIMITS.noteChars));
    mode("Address");
    send();
    expect(screen.getByRole("alert").textContent).toBe("Write a web address.");
    type("Web address", "plugin:something");
    send();
    expect(screen.getByRole("alert").textContent).toBe("That is not a web address. Use one that starts with http:// or https://.");
    mode("File");
    send();
    expect(screen.getByRole("alert").textContent).toBe("Choose a file.");
    expect(sent(calls)).toHaveLength(0);
  });

  it("sends a note with Ctrl+Enter from its text box, where Enter is a new line", async () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);
    type("Title", "T");
    type("Text", "text");

    fireEvent.keyDown(screen.getByLabelText("Text"), { key: "Enter" });
    expect(sent(calls)).toHaveLength(0);
    fireEvent.keyDown(screen.getByLabelText("Text"), { key: "Enter", ctrlKey: true });

    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)).toHaveLength(1);
  });

  it("sends an address, and says when it was already there", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.ingest": ok({ ...CREATED, created: 0, unchanged: 1, chunks: 0 }),
    });
    renderForm(transport);
    mode("Address");

    type("Web address", " https://example.com/a ");
    send();

    await screen.findByText("Already in the research base.");
    expect(sent(calls)[0]?.payload).toEqual({ mode: "address", address: "https://example.com/a" });
    await waitFor(() => expect((screen.getByLabelText("Web address") as HTMLInputElement).value).toBe(""));
  });

  it("says when it replaced an earlier version", async () => {
    const { transport } = routedTransport({ "knowledge.ingest": ok({ ...CREATED, created: 0, updated: 1 }) });
    renderForm(transport);
    mode("Address");
    type("Web address", "https://example.com/a");
    send();

    await screen.findByText("Updated the earlier version. Passages stored: 2.");
  });

  it("reads a file when it is chosen, and sends it when told to", async () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport);
    mode("File");

    chooseFile(new File(["hello"], "reading.txt", { type: "text/plain" }));
    await screen.findByText("reading.txt, 1 KB");
    expect(sent(calls)).toHaveLength(0);

    send();
    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)[0]?.payload).toEqual({ mode: "file", filename: "reading.txt", content: "aGVsbG8=" });
    await waitFor(() => expect(screen.queryByText("reading.txt, 1 KB")).toBeNull());
  });

  it("takes a file dropped on the form, whatever kind was open", async () => {
    const { transport, calls } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    const view = renderForm(transport);

    fireEvent.drop(view.container.querySelector("form") as HTMLFormElement, {
      dataTransfer: { files: [new File(["hello"], "dropped.md")] },
    });

    await screen.findByText("dropped.md, 1 KB");
    expect(screen.getByRole("button", { name: "File" }).getAttribute("aria-pressed")).toBe("true");
    send();
    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)[0]?.payload?.filename).toBe("dropped.md");
  });

  it("refuses a file that is too big or empty, saying why", async () => {
    renderForm(routedTransport({}).transport);
    mode("File");

    chooseFile(new File([new Uint8Array(INGEST_LIMITS.fileBytes + 1)], "big.pdf"));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("The file is larger than 8 MB."));
    chooseFile(new File([], "empty.txt"));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("The file could not be read."));
    expect(screen.queryByText(/KB$/)).toBeNull();
  });

  it("is off while it sends, and never sends twice", async () => {
    const slow = deferred<TransportResult>();
    const { transport, calls } = routedTransport({ "knowledge.ingest": () => slow.promise });
    renderForm(transport);
    type("Title", "T");
    type("Text", "text");

    send();
    await screen.findByText("Adding…");
    expect((screen.getByLabelText("Title").closest("fieldset") as HTMLFieldSetElement).disabled).toBe(true);
    fireEvent.submit(screen.getByLabelText("Title").closest("form") as HTMLFormElement);
    fireEvent.keyDown(screen.getByLabelText("Text"), { key: "Enter", ctrlKey: true });
    expect(sent(calls)).toHaveLength(1);

    await act(async () => {
      slow.resolve(ok(CREATED));
    });
    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)).toHaveLength(1);
  });

  it("keeps what was typed when it fails, and says a timeout may still have worked", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.ingest": [failure("timeout"), failure("unavailable"), ok(CREATED)],
    });
    renderForm(transport);
    type("Title", "T");
    type("Text", "text");

    send();
    await screen.findByText("No answer in time. It may have been added; sending it again is safe.");
    expect((screen.getByLabelText("Text") as HTMLTextAreaElement).value).toBe("text");

    send();
    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("T");

    send();
    await screen.findByText("Added. Passages stored: 2.");
    expect(sent(calls)).toHaveLength(3);
  });

  it("says what the service said when it refuses", async () => {
    const { transport } = routedTransport({ "knowledge.ingest": failure("orchestrator", "that is not a syndication feed") });
    renderForm(transport);
    mode("Address");
    type("Web address", "https://example.com/a");
    send();

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator answered with an error: that is not a syndication feed");
  });

  it("says when the passages are not indexed by meaning, and counts what a feed held", async () => {
    const { transport } = routedTransport({
      "knowledge.ingest": ok({ count: 5, created: 3, updated: 1, unchanged: 1, chunks: 9, degraded: true, reason: "no embedding server is configured" }),
    });
    renderForm(transport);
    mode("Address");
    type("Web address", "https://example.com/feed");
    send();

    await screen.findByText("5 entries: 3 added, 1 updated, 1 already there.");
    screen.getByText("Not indexed by meaning yet, so for now it is found by keywords.");
    screen.getByText("no embedding server is configured");
  });

  it("says a switched-off service is switched off", async () => {
    const { transport } = routedTransport({ "knowledge.ingest": ok(OFF) });
    renderForm(transport);
    type("Title", "T");
    type("Text", "text");
    send();

    await screen.findByText("Switched off.");
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport({ "knowledge.ingest": ok(CREATED) });
    renderForm(transport, "pt");

    screen.getByRole("heading", { name: "Adicionar à base de pesquisa" });
    fireEvent.click(screen.getByRole("button", { name: "Adicionar" }));
    expect(screen.getByRole("alert").textContent).toBe("Uma nota precisa de um título.");
    fireEvent.change(screen.getByLabelText("Título"), { target: { value: "T" } });
    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "texto" } });
    fireEvent.click(screen.getByRole("button", { name: "Adicionar" }));

    await screen.findByText("Adicionado. Trechos guardados: 2.");
    within(screen.getByRole("group", { name: "Adicionar à base de pesquisa" })).getByRole("button", { name: "Endereço" });
  });
});
