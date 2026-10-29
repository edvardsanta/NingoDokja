import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../../src/renderer/app.js";
import { AssistantCard } from "../../src/renderer/cards/assistant.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { ChatPreview, Transport, TransportResult } from "../../src/shared/transport.js";
import { answering, archiveId, archivedAnswer, archivedTurn, deferred, failure, ok, routedTransport, startedConversation } from "./support.js";

function renderTalk(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <AssistantCard transport={transport} />
    </I18nProvider>,
  );
}

const field = () => screen.getByRole("textbox", { name: "Your message" }) as HTMLTextAreaElement;
const sent = (calls: { type: string; payload: Record<string, unknown> | undefined }[]) =>
  calls.filter((call) => call.type === "message.created").map((call) => call.payload);
// The archive keeps each turn under the send id the screen named it by.
const sendIdOf = (calls: { type: string; payload: Record<string, unknown> | undefined }[]) => String(sent(calls)[0]?.send_id ?? "");

describe("the conversation", () => {
  it("opens with Ningo's line, then a plain next step, and its message docked at the foot", () => {
    const { transport } = routedTransport({});
    renderTalk(transport);

    const voice = screen.getByText("I've read more than you'd guess. Ask.");
    expect(voice.className).toBe("voice");
    expect(voice.nextElementSibling?.textContent).toBe("Ask anything, or start from one of these and finish the thought.");
    expect(field().closest(".wp-dock")).not.toBeNull();
    expect(field().placeholder).toBe("Ask Ningo…");
    // Sending is the one main action of the dock, drawn like the other main actions.
    expect(screen.getByRole("button", { name: "Send" }).className).toBe("primary");
  });

  it("fills the message from a beginning and sends nothing until the owner does", () => {
    const { transport, calls } = routedTransport({ "message.created": answering("Then argue.") });
    renderTalk(transport);

    fireEvent.click(screen.getByRole("button", { name: "Argue against:" }));
    expect(field().value).toBe("Argue against: ");
    expect(document.activeElement).toBe(field());
    expect(calls).toHaveLength(0);
  });

  it("sends on Enter, breaks the line on Shift+Enter, and keeps one id for the whole conversation", async () => {
    const { transport, calls } = routedTransport({ "conversation.start": startedConversation(), "message.created": answering("First answer.", "Second answer.") });
    renderTalk(transport);

    fireEvent.change(field(), { target: { value: "first" } });
    fireEvent.keyDown(field(), { key: "Enter", shiftKey: true });
    expect(calls).toHaveLength(0);
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("First answer.");
    expect(screen.queryByText("I've read more than you'd guess. Ask.")).toBeNull();

    fireEvent.change(field(), { target: { value: "second" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText("Second answer.");

    const payloads = sent(calls);
    expect(payloads.map((payload) => payload?.content)).toEqual(["first", "second"]);
    expect(payloads[0]?.conversation_id).toBe(archiveId(1));
    expect(payloads[1]?.conversation_id).toBe(archiveId(1));
    // Each message has its own send id, so the archive answers each once.
    expect(payloads[0]?.send_id).toEqual(expect.any(String));
    expect(payloads[0]?.send_id).not.toBe(payloads[1]?.send_id);
    expect(calls.filter((call) => call.type === "conversation.start")).toHaveLength(1);
  });

  it("shows the reply as Ningo writes it, for this conversation only, and the reply replaces it", async () => {
    let listener: ((preview: ChatPreview) => void) | undefined;
    let answer: (value: TransportResult) => void = () => {};
    let conversationId = "";
    let sentPayload: Record<string, unknown> | undefined;
    const transport: Transport = {
      request: (type, payload) => {
        if (type === "conversation.start") return Promise.resolve(startedConversation());
        if (type !== "message.created") return Promise.resolve(ok({}));
        conversationId = String(payload?.conversation_id);
        sentPayload = payload;
        return new Promise((resolve) => (answer = resolve));
      },
      preview: async () => ({ ok: false, error: { code: "unavailable", message: "none" } }),
      onChatPreview: (each) => {
        listener = each;
        return () => {
          listener = undefined;
        };
      },
    };
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "question" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Ningo is thinking…");
    await waitFor(() => expect(listener).toBeDefined());

    act(() => listener?.({ conversationId: "another", text: "Not yours" }));
    expect(screen.queryByText("Not yours")).toBeNull();
    act(() => listener?.({ conversationId, text: "The ans" }));
    expect(screen.getByText("The ans").closest(".assistant-preview")).not.toBeNull();
    expect(screen.queryByText("Ningo is thinking…")).toBeNull();

    act(() => answer(archivedAnswer("The answer.", sentPayload)));
    await screen.findByText("The answer.");
    expect(screen.queryByText("The ans")).toBeNull();
    expect(listener).toBeUndefined();
  });

  it("drops a reply still on its way when a new conversation starts", async () => {
    const late = deferred<TransportResult>();
    const { transport } = routedTransport({ "conversation.start": startedConversation(), "message.created": () => late.promise });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "first" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Ningo is thinking…");
    fireEvent.click(screen.getByRole("button", { name: "Start a new conversation" }));
    await act(async () => late.resolve(archivedAnswer("A reply to the old conversation.", { content: "first" })));
    expect(screen.queryByText("A reply to the old conversation.")).toBeNull();
    expect(screen.queryByText("first")).toBeNull();
    screen.getByText("I've read more than you'd guess. Ask.");
  });

  it("starts a new conversation with a new id and an empty transcript", async () => {
    const { transport, calls } = routedTransport({ "conversation.start": [startedConversation(1), startedConversation(2)], "message.created": answering("Noted.") });
    renderTalk(transport);

    fireEvent.change(field(), { target: { value: "hello" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Noted.");
    fireEvent.click(screen.getByRole("button", { name: "Start a new conversation" }));
    expect(screen.queryByText("Noted.")).toBeNull();
    screen.getByText("I've read more than you'd guess. Ask.");

    fireEvent.change(field(), { target: { value: "again" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await waitFor(() => expect(sent(calls)).toHaveLength(2));
    const [first, second] = sent(calls);
    expect(first?.conversation_id).toBe(archiveId(1));
    expect(second?.conversation_id).toBe(archiveId(2));
  });

  it("starts no conversation until the first message, so opening Talk records nothing", () => {
    const { transport, calls } = routedTransport({});
    renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Start a new conversation" }));
    expect(calls).toHaveLength(0);
  });

  it("gives the message back unsent when the history is unavailable, and answers nothing unkept", async () => {
    const { transport, calls } = routedTransport({ "conversation.start": failure("unavailable") });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "keep this" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    await screen.findByText(/Conversation history is unavailable/);
    expect(field().value).toBe("keep this");
    expect(sent(calls)).toHaveLength(0);
    expect(screen.queryByText("keep this", { selector: "p" })).toBeNull();
  });

  it("retries a failed message as the same turn, showing the owner's message once", async () => {
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": [failure("orchestrator", "the provider failed"), async () => ok({ reply: "At last.", replayed: false, turn: archivedTurn({ content: "why?", reply: "At last.", attempts: 2, sendId: sendIdOf(calls) }) })],
      "conversation.read": async () => ok({ conversation: {}, turns: [archivedTurn({ content: "why?", state: "failed", reason: "the provider failed", sendId: sendIdOf(calls) })] }),
    });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "why?" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    fireEvent.click(await screen.findByRole("button", { name: "Retry the last message" }));
    await screen.findByText("At last.");
    expect(screen.getAllByText("why?")).toHaveLength(1);
    const [, retried] = sent(calls);
    expect(retried).toEqual({ conversation_id: archiveId(1), retry_turn_id: archiveId(0x101) });
  });

  it("reads the archive back after a timeout and shows a reply that landed, without sending again", async () => {
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": failure("timeout"),
      "conversation.read": async () => ok({ conversation: {}, turns: [archivedTurn({ content: "slow one", reply: "Late, but written.", sendId: sendIdOf(calls) })] }),
    });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "slow one" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    await screen.findByText("Late, but written.");
    expect(screen.getAllByText("slow one")).toHaveLength(1);
    expect(sent(calls)).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Retry the last message" })).toBeNull();
  });

  it("sends a message again under its own send id when its outcome could not be read", async () => {
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": [failure("unavailable"), answering("Answered once.")],
      "conversation.read": failure("unavailable"),
    });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "lost?" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    fireEvent.click(await screen.findByRole("button", { name: "Retry the last message" }));
    await screen.findByText("Answered once.");
    const [first, again] = sent(calls);
    expect(again).toEqual({ conversation_id: archiveId(1), content: "lost?", send_id: first?.send_id });
    expect(screen.getAllByText("lost?")).toHaveLength(1);
  });

  it("checks again, without sending, when a message is still on its way", async () => {
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": failure("timeout"),
      "conversation.read": [
        async () => ok({ conversation: {}, turns: [archivedTurn({ content: "wait", state: "pending", sendId: sendIdOf(calls) })] }),
        async () => ok({ conversation: {}, turns: [archivedTurn({ content: "wait", reply: "Done now.", sendId: sendIdOf(calls) })] }),
      ],
    });
    renderTalk(transport);
    fireEvent.change(field(), { target: { value: "wait" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    await screen.findByText("This message is still on its way.");
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    await screen.findByText("Done now.");
    expect(sent(calls)).toHaveLength(1);
  });

  it("speaks Portuguese as Ningo, in the first person", () => {
    const { transport } = routedTransport({});
    renderTalk(transport, "pt");

    screen.getByText("Li mais do que você imagina. Pergunte.");
    expect((screen.getByRole("textbox", { name: "Sua mensagem" }) as HTMLTextAreaElement).placeholder).toBe("Pergunte ao Ningo…");
    screen.getByText("Enter envia · Shift+Enter quebra a linha");
  });

  it("says plainly when nothing answers and opens System from there", async () => {
    const { transport } = routedTransport({ "ningo.status": failure("unavailable") });
    render(<App transport={transport} locale="en" />);

    // the screen falls back to System on its own; come back to the conversation to see what it says
    await waitFor(() => expect(screen.getByRole("tab", { name: "System" }).getAttribute("aria-selected")).toBe("true"));
    fireEvent.click(screen.getByRole("tab", { name: "Talk" }));
    screen.getByText("Ningo is not answering. The orchestrator isn't running or can't be reached.");
    fireEvent.click(screen.getByRole("button", { name: "Open System" }));
    expect(screen.getByRole("tab", { name: "System" }).getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "System" }));
  });
});
