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
    fireEvent.click(screen.getByRole("button", { name: "Older messages" }));
    await screen.findByText("older");
    fireEvent.click(screen.getByRole("button", { name: "Latest messages" }));
    await screen.findByText("<script>text</script>");
    expect(calls.filter((call) => call.type === "communications.history").map((call) => call.payload?.before)).toEqual(["", "10", ""]);
  });
  it("asks again for an older page that failed, without going back to latest", async () => {
    const { transport, calls } = routedTransport({ "communications.channels": ok(channels), "communications.history": [ok(history("123", "10")), failure("orchestrator", "rate_limited"), ok({ ...history(), messages: [message("5", "older")] })] });
    render(view(transport));
    await screen.findByText("<script>text</script>");
    fireEvent.click(screen.getByRole("button", { name: "Older messages" }));
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Older messages" }));
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
    fireEvent.click(await screen.findByRole("button", { name: "Load memes" }));
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
    expect(calls.find((call) => call.type === "communications.send")?.payload).toEqual({ channel_id: "123", content: "", attachment_url: "https://images.example/a.png" });
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
    }, preview);
    render(view(transport));
    fireEvent.click(await screen.findByRole("button", { name: "Load memes" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Attach First" }));
    const input = screen.getByPlaceholderText("#tag") as HTMLInputElement;
    expect(screen.getByRole("button", { name: "#reaction" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "#cat" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "#reaction" }));
    expect(input.value).toBe("#reaction");
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
    fireEvent.click(await screen.findByRole("button", { name: "Load memes" }));
    const tile = await screen.findByRole("button", { name: "Attach First" });
    await waitFor(() => expect(tile.querySelector("img")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Attach First" }));
    fireEvent.change(screen.getByPlaceholderText("#tag"), { target: { value: "#reaction" } });
    fireEvent.click(screen.getByRole("button", { name: "Create tag" }));
    await screen.findByDisplayValue("#reaction");
    expect(calls.some((call) => call.type === "communications.send")).toBe(false);
    expect((screen.getByRole("button", { name: "Send as Ningo" }) as HTMLButtonElement).disabled).toBe(false);
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
