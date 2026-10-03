import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommunicationsCard } from "../../src/renderer/cards/communications.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { deferred, failure, ok, routedTransport } from "./support.js";

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
    expect((screen.getByLabelText("Channel") as HTMLSelectElement).disabled).toBe(true);
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
    fireEvent.change(await screen.findByLabelText("Channel"), { target: { value: "456" } });
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
});
