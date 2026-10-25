import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommunicationsCard } from "../../src/renderer/cards/communications.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { deferred, failure, ok, previewer, routedTransport } from "./support.js";

const channels = { channels: [{ id: "123", name: "reading" }, { id: "456", name: "memes" }] };
const message = (id = "10", content = "<script>text</script>") => ({ id, author: "Reader", bot: false, content, timestamp: new Date().toISOString(), edited: false, replyTo: "", attachments: ["clip.mp4"] });
const history = (channelId = "123", before = "") => ({ channelId, messages: [message()], before });
const view = (transport: Transport, active = true) => <I18nProvider locale="en"><CommunicationsCard transport={transport} active={active} /></I18nProvider>;
afterEach(() => vi.useRealTimers());

describe("communications", () => {
  it("lists channels and treats untrusted messages as text", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()) });
    const { container } = render(view(transport));
    await screen.findByText("<script>text</script>");
    expect(container.querySelector("script")).toBeNull();
    screen.getByText("Attachment: clip.mp4");
    expect(calls.map((call) => call.type)).toEqual(["communications.channels", "communications.history"]);
  });
  it("pages with the returned cursor and returns to latest", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": [ok(history("123", "10")), ok({ ...history(), messages: [message("5", "older")] }), ok(history())] });
    render(view(transport));
    await screen.findByText("<script>text</script>");
    fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    await screen.findByText("older");
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("<script>text</script>");
    expect(calls.filter((call) => call.type === "communications.history").map((call) => call.payload?.before)).toEqual(["", "10", ""]);
  });
  it("asks again for an older page that failed, without going back to latest", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": [ok(history("123", "10")), failure("orchestrator", "rate_limited"), ok({ ...history(), messages: [message("5", "older")] })] });
    render(view(transport));
    await screen.findByText("<script>text</script>");
    fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
    await screen.findByText("older");
    expect(calls.filter((call) => call.type === "communications.history").map((call) => call.payload?.before)).toEqual(["", "10", "10"]);
  });
  it("preserves the draft on cancellation or timeout without automatic retries", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "communications.send": [failure("cancelled"), failure("timeout")] });
    render(view(transport));
    const field = await screen.findByLabelText("Message to send as Ningo") as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "hello" } });
    fireEvent.click(screen.getByRole("button", { name: "Send as Ningo" }));
    await screen.findByRole("alert");
    expect(field.value).toBe("hello");
    fireEvent.click(screen.getByRole("button", { name: "Send as Ningo" }));
    await screen.findByText(/Check the latest messages before sending again/);
    expect(field.value).toBe("hello");
    expect(calls.filter((call) => call.type === "communications.send")).toHaveLength(2);
  });
  it("locks the channel and duplicate submits while sending, then clears and refreshes", async () => {
    const pending = deferred<TransportResult>();
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "communications.send": () => pending.promise });
    render(view(transport));
    const field = await screen.findByLabelText("Message to send as Ningo") as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "hello" } });
    const button = screen.getByRole("button", { name: "Send as Ningo" });
    fireEvent.click(button); fireEvent.click(button);
    const list = screen.getByRole("navigation", { name: "Channels and conversations" });
    for (const entry of within(list).getAllByRole("button")) expect((entry as HTMLButtonElement).disabled).toBe(true);
    pending.resolve(ok({ sent: true, skipped: false }));
    await screen.findByText("Message sent.");
    expect(field.value).toBe("");
    expect(calls.filter((call) => call.type === "communications.send")).toHaveLength(1);
    await waitFor(() => expect(calls.filter((call) => call.type === "communications.history")).toHaveLength(2));
  });
  it("drops late results from the previous channel", async () => {
    const pending = deferred<TransportResult>();
    const { transport } = routedTransport({ "communications.channels": ok(channels), "communications.history": [() => pending.promise, ok({ ...history("456"), messages: [message("20", "second channel")] })] });
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "#memes · 456" }));
    await screen.findByText("second channel");
    await act(async () => pending.resolve(ok(history())));
    expect(screen.queryByText("<script>text</script>")).toBeNull();
  });
  it("polls only the active latest page and pauses after errors", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": [ok(history()), failure("orchestrator", "rate_limited")] });
    const rendered = render(view(transport));
    await screen.findByText("<script>text</script>");
    vi.useFakeTimers();
    rendered.rerender(view(transport, false));
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(calls.filter((call) => call.type === "communications.history")).toHaveLength(1);
    rendered.rerender(view(transport, true));
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(calls.filter((call) => call.type === "communications.history")).toHaveLength(2);
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(calls.filter((call) => call.type === "communications.history")).toHaveLength(2);
  });
  it("does not request history for empty configuration", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok({ channels: [] }) });
    render(view(transport));
    await screen.findByText("No delivery channels are configured.");
    expect(calls).toHaveLength(1);
  });
  it("attaches a meme picked from the queue's previews, and takes it off on a second click", async () => {
    const memePage = { total: 2, offset: 0, memes: [
      { url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" },
      { url: "", title: "No address", tags: "", source: "", createdAt: "", sentAt: "" },
    ] };
    const { asked, preview } = previewer();
    const { transport, calls } = routedTransport({
      "communications.channels": ok(channels),
      "communications.history": ok(history()),
      "meme.list": ok(memePage),
      "communications.send": ok({ sent: true }),
    }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    expect(screen.getByRole("list", { name: "Memes to attach" }).querySelectorAll("li")).toHaveLength(1);
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    expect(asked).toEqual(["https://images.example/a.png"]);

    const attachButton = () => screen.getByRole("button", { name: "Attach First" });
    fireEvent.click(attachButton());
    expect(attachButton().getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(attachButton());
    expect(attachButton().getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(attachButton());
    fireEvent.click(screen.getByRole("button", { name: "Send as Ningo" }));
    await screen.findByText("Message sent.");
    expect(calls.find((call) => call.type === "communications.send")?.payload).toEqual({ channel_id: "123", content: "", attachment_url: "https://images.example/a.png", skip_hashtag: true });
  });
  it("says plainly that messages are not set up, and what to set, instead of the raw error", async () => {
    const { transport } = routedTransport({ "communications.channels": failure("orchestrator", "Communications access is not configured.") });
    render(view(transport));
    await screen.findByText("Messages are not set up yet.");
    screen.getByText(/DOKJA_COMMUNICATIONS_TOKEN/);
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("opens the memes from the composer, marks the attached one, shows one large apart, and closes", async () => {
    const memePage = { total: 1, offset: 0, memes: [{ url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" }] };
    const { preview } = previewer();
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "meme.list": ok(memePage) }, preview);
    render(view(transport));
    const toggle = await screen.findByRole("button", { name: "Attach a meme" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Show First larger" }));
    expect(tile.getAttribute("aria-pressed")).toBe("false");
    screen.getByRole("dialog", { name: "First" });
    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));

    fireEvent.click(tile);
    expect(tile.closest(".meme-tile")?.querySelector(".meme-badge")?.textContent).toBe("Attached");
    fireEvent.click(toggle);
    expect(screen.queryByRole("list", { name: "Memes to attach" })).toBeNull();
    expect(calls.filter((call) => call.type === "meme.list")).toHaveLength(1);
  });
  it("offers existing meme tags as suggestions when tagging a meme", async () => {
    const memePage = { total: 1, offset: 0, memes: [
      { url: "https://images.example/a.png", title: "First", tags: "reaction, #cat", source: "", createdAt: "", sentAt: "" },
    ] };
    const { preview } = previewer();
    const { transport } = routedTransport({
      "communications.channels": ok(channels),
      "communications.history": ok(history()),
      "meme.list": ok(memePage),
      "knowledge.list": ok({ total: 2, offset: 0, documents: [
        { id: "meme-hashtag:1", title: "#reaction", kind: "meme_hashtag", reference: "", tags: [], chunks: 1, embedded: 1, updatedAt: "" },
        { id: "meme-hashtag:2", title: "#cat", kind: "meme_hashtag", reference: "", tags: [], chunks: 1, embedded: 1, updatedAt: "" },
      ] }),
      "memory.list": ok({ total: 1, offset: 0, limit: 100, experiences: [
        { ref: "experience:1", action: "hashtag.suggest", detail: "#PedroFutebol", outcome: "accepted" },
      ] }),
    }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Attach First" }));
    const input = screen.getByPlaceholderText("#tag") as HTMLInputElement;
    expect(screen.getByRole("button", { name: "#reaction" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "#cat" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "#PedroFutebol" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "#reaction" }));
    expect(input.value).toBe("#reaction");
    fireEvent.change(input, { target: { value: "" } });
    expect(input.value).toBe("");
    expect((screen.getByRole("textbox", { name: "Message to send as Ningo" }) as HTMLTextAreaElement).value).toBe("");
  });
  it("fills the composer with the newly created hashtag and meme", async () => {
    const memePage = { total: 1, offset: 0, memes: [
      { url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" },
    ] };
    const memeUrl = "https://images.example/a.png";
    const { preview } = previewer();
    const { transport, calls } = routedTransport({
      "communications.channels": ok(channels),
      "communications.history": ok(history()),
      "meme.list": ok(memePage),
      "meme.hashtag.tag": ok({ source_url: memeUrl, hashtag: "#reaction", embedded: true }),
    }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Attach First" }));
    fireEvent.change(screen.getByPlaceholderText("#tag"), { target: { value: "#reaction" } });
    fireEvent.click(screen.getByRole("button", { name: "Create tag" }));
    await screen.findByDisplayValue("#reaction");
    expect(calls.some((call) => call.type === "communications.send")).toBe(false);
    expect((screen.getByRole("button", { name: "Send as Ningo" }) as HTMLButtonElement).disabled).toBe(false);
  });
  it("asks for the meme's text when it cannot be read from the image, and tags with what the owner wrote", async () => {
    const memePage = { total: 1, offset: 0, memes: [
      { url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" },
    ] };
    const { preview } = previewer();
    const { transport, calls } = routedTransport({
      "communications.channels": ok(channels),
      "communications.history": ok(history()),
      "meme.list": ok(memePage),
      "meme.hashtag.tag": [
        failure("orchestrator", 'dispatch workflow: dispatch meme: unavailable: no enabled provider of media.perceive.v1 for "read-text"'),
        ok({ source_url: "https://images.example/a.png", hashtag: "#reaction", embedded: true }),
      ],
    }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(tile);
    expect(screen.queryByRole("textbox", { name: "The meme's text" })).toBeNull();
    fireEvent.change(screen.getByPlaceholderText("#tag"), { target: { value: "#reaction" } });
    fireEvent.click(screen.getByRole("button", { name: "Create tag" }));

    const problem = await screen.findByRole("alert");
    expect(problem.textContent).toMatch(/^Reading images is not installed/);
    expect(problem.getAttribute("title")).toMatch(/no enabled provider of media\.perceive\.v1/);
    fireEvent.change(screen.getByRole("textbox", { name: "The meme's text" }), { target: { value: "  when the build is green  " } });
    fireEvent.click(screen.getByRole("button", { name: "Create tag" }));
    await screen.findByDisplayValue("#reaction");
    const tags = calls.filter((call) => call.type === "meme.hashtag.tag").map((call) => call.payload);
    expect(tags).toEqual([
      { url: "https://images.example/a.png", hashtag: "#reaction" },
      { url: "https://images.example/a.png", hashtag: "#reaction", text: "when the build is green" },
    ]);
  });
  it("keeps the attached meme as a chip when the tray closes, and takes it off with the cross", async () => {
    const memePage = { total: 30, offset: 0, memes: [
      { url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" },
      { url: "https://images.example/b.png", title: "Second", tags: "", source: "", createdAt: "", sentAt: "" },
    ] };
    const { preview } = previewer();
    const { transport } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "meme.list": ok(memePage) }, preview);
    const { container } = render(view(transport));
    const toggle = await screen.findByRole("button", { name: "Attach a meme" });
    const send = screen.getByRole("button", { name: "Send as Ningo" }) as HTMLButtonElement;
    expect(container.querySelector(".attached-meme")).toBeNull();
    expect(send.disabled).toBe(true);
    fireEvent.click(toggle);
    await screen.findByText("1–2 of 30");
    fireEvent.click(await screen.findByRole("button", { name: "Attach Second" }));

    const chip = container.querySelector(".attached-meme") as HTMLElement;
    expect(within(chip).getByText("Second")).toBeTruthy();
    expect(send.disabled).toBe(false);
    // the tray is only for picking: shut, the message still says what it carries
    fireEvent.click(toggle);
    expect(screen.queryByRole("list", { name: "Memes to attach" })).toBeNull();
    expect(container.querySelector(".attached-meme")).toBe(chip);

    fireEvent.click(within(chip).getByRole("button", { name: "Remove Second" }));
    expect(container.querySelector(".attached-meme")).toBeNull();
    expect(send.disabled).toBe(true);
  });
  it("offers sending past the media screening only on the attached meme, and forgets it when the meme goes", async () => {
    const memePage = { total: 1, offset: 0, memes: [{ url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" }] };
    const { preview } = previewer();
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "meme.list": ok(memePage), "communications.send": ok({ sent: true }) }, preview);
    const { container } = render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    expect(screen.queryByRole("checkbox")).toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "Attach First" }));
    const force = within(container.querySelector(".attached-meme") as HTMLElement).getByRole("checkbox", { name: "Force send (bypass media screening)" }) as HTMLInputElement;
    fireEvent.click(force);
    expect(force.checked).toBe(true);

    // taking the meme off takes its option with it: a later meme starts with the screening on
    fireEvent.click(screen.getByRole("button", { name: "Remove First" }));
    fireEvent.click(screen.getByRole("button", { name: "Attach First" }));
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(false);
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Send as Ningo" }));
    await screen.findByText("Message sent.");
    expect(calls.find((call) => call.type === "communications.send")?.payload).toEqual({ channel_id: "123", content: "", attachment_url: "https://images.example/a.png", skip_hashtag: true, force_nsfw: true });
    // sent: the draft is done, and the tray is shut so the history shows the message arriving
    expect(container.querySelector(".attached-meme")).toBeNull();
    expect(screen.queryByRole("list", { name: "Memes to attach" })).toBeNull();
    expect(screen.getByRole("button", { name: "Attach a meme" }).getAttribute("aria-expanded")).toBe("false");
    // and "sent" is about that message: it goes as soon as the next one is begun
    fireEvent.change(screen.getByLabelText("Message to send as Ningo"), { target: { value: "next" } });
    expect(screen.queryByText("Message sent.")).toBeNull();
  });
  it("pages the memes in the tray with the offset it was asked for", async () => {
    const meme = (number: number) => ({ url: `https://images.example/${number}.png`, title: `Meme ${number}`, tags: "", source: "", createdAt: "", sentAt: "" });
    const first = { total: 24, offset: 0, memes: Array.from({ length: 12 }, (_, index) => meme(index + 1)) };
    const second = { total: 24, offset: 12, memes: Array.from({ length: 12 }, (_, index) => meme(index + 13)) };
    const { preview } = previewer();
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "meme.list": [ok(first), ok(second), ok(first)] }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Attach a meme" }));
    await screen.findByText("1–12 of 24");
    expect((screen.getByRole("button", { name: "Previous memes" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Next memes" }));
    await screen.findByText("13–24 of 24");
    screen.getByRole("button", { name: "Attach Meme 13" });
    expect((screen.getByRole("button", { name: "Next memes" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Previous memes" }));
    await screen.findByText("1–12 of 24");
    expect(calls.filter((call) => call.type === "meme.list").map((call) => call.payload?.offset)).toEqual([0, 12, 0]);
  });
  it("closes the tray with Escape and puts the focus back on its button", async () => {
    const memePage = { total: 1, offset: 0, memes: [{ url: "https://images.example/a.png", title: "First", tags: "", source: "", createdAt: "", sentAt: "" }] };
    const { preview } = previewer();
    const { transport } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()), "meme.list": ok(memePage) }, preview);
    render(view(transport));
    const toggle = await screen.findByRole("button", { name: "Attach a meme" });
    fireEvent.click(toggle);
    const tile = await screen.findByRole("button", { name: "Attach First" });
    // opened, the tray takes the focus to its first meme
    expect(document.activeElement).toBe(tile);

    // with the large view open, Escape is for the view alone
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Show First larger" }));
    screen.getByRole("dialog", { name: "First" });
    fireEvent.keyDown(tile, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    screen.getByRole("list", { name: "Memes to attach" });

    fireEvent.keyDown(tile, { key: "Escape" });
    expect(screen.queryByRole("list", { name: "Memes to attach" })).toBeNull();
    expect(document.activeElement).toBe(toggle);
  });
  it("opens the history at its last message, and does not pull a reader back who scrolled up", async () => {
    // jsdom lays nothing out: give the history a height so there is an end to be at
    const height = vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(500);
    try {
      const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": ok(history()) });
      const { container } = render(view(transport));
      await screen.findByText("<script>text</script>");
      const box = container.querySelector(".thread-history") as HTMLElement;
      expect(box.scrollTop).toBe(500);

      box.scrollTop = 100;
      fireEvent.scroll(box);
      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
      await waitFor(() => expect(calls.filter((call) => call.type === "communications.history")).toHaveLength(2));
      await act(async () => { await Promise.resolve(); });
      expect(box.scrollTop).toBe(100);
    } finally { height.mockRestore(); }
  });
  it("lists the conversations beside the chosen one, and marks which one is open", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": [ok(history()), ok({ ...history("456"), messages: [message("20", "second channel")] })] });
    render(view(transport));
    await screen.findByText("<script>text</script>");
    const list = screen.getByRole("navigation", { name: "Channels and conversations" });
    expect(within(list).getAllByRole("button").map((entry) => entry.textContent)).toEqual(["#reading · 123", "#memes · 456"]);
    expect(within(list).getByRole("button", { name: "#reading · 123" }).getAttribute("aria-current")).toBe("true");

    fireEvent.click(within(list).getByRole("button", { name: "#memes · 456" }));
    await screen.findByText("second channel");
    expect(within(list).getByRole("button", { name: "#memes · 456" }).getAttribute("aria-current")).toBe("true");
    expect(within(list).getByRole("button", { name: "#reading · 123" }).hasAttribute("aria-current")).toBe(false);
    expect(calls.filter((call) => call.type === "communications.history").map((call) => call.payload?.channel_id)).toEqual(["123", "456"]);
  });
});
