import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "../../src/renderer/app.js";
import { AssistantCard } from "../../src/renderer/cards/assistant.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { ChatPreview, Transport, TransportResult } from "../../src/shared/transport.js";
import { answering, archiveId, archivedAnswer, archivedTurn, deferred, failure, ok, routedTransport, startedConversation } from "./support.js";

// Talk first looks for a conversation to continue; with none, it opens on a new one.
async function renderTalk(transport: Transport, locale: Locale = "en") {
  const view = render(
    <I18nProvider locale={locale}>
      <AssistantCard transport={transport} />
    </I18nProvider>,
  );
  await waitFor(() => expect(document.querySelector('[role="status"]')?.textContent ?? "").not.toMatch(/Opening the conversation|Abrindo a conversa/));
  return view;
}

// What Talk asked beyond looking for a conversation to continue.
const asked = (calls: { type: string }[]) => calls.filter((call) => call.type !== "conversation.list");

const field = () => screen.getByRole("textbox", { name: "Your message" }) as HTMLTextAreaElement;
const sent = (calls: { type: string; payload: Record<string, unknown> | undefined }[]) =>
  calls.filter((call) => call.type === "message.created").map((call) => call.payload);
// The archive keeps each turn under the send id the screen named it by.
const sendIdOf = (calls: { type: string; payload: Record<string, unknown> | undefined }[]) => String(sent(calls)[0]?.send_id ?? "");

describe("the conversation", () => {
  it("opens with Ningo's line, then a plain next step, and its message docked at the foot", async () => {
    const { transport } = routedTransport({});
    await renderTalk(transport);

    const voice = screen.getByText("I've read more than you'd guess. Ask.");
    expect(voice.className).toBe("voice");
    expect(voice.nextElementSibling?.textContent).toBe("Ask anything, or start from one of these and finish the thought.");
    expect(field().closest(".wp-dock")).not.toBeNull();
    expect(field().placeholder).toBe("Ask Ningo…");
    // Sending is the one main action of the dock, drawn like the other main actions.
    expect(screen.getByRole("button", { name: "Send" }).className).toBe("primary");
  });

  it("fills the message from a beginning and sends nothing until the owner does", async () => {
    const { transport, calls } = routedTransport({ "message.created": answering("Then argue.") });
    await renderTalk(transport);

    fireEvent.click(screen.getByRole("button", { name: "Argue against:" }));
    expect(field().value).toBe("Argue against: ");
    expect(document.activeElement).toBe(field());
    expect(asked(calls)).toHaveLength(0);
  });

  it("sends on Enter, breaks the line on Shift+Enter, and keeps one id for the whole conversation", async () => {
    const { transport, calls } = routedTransport({ "conversation.start": startedConversation(), "message.created": answering("First answer.", "Second answer.") });
    await renderTalk(transport);

    fireEvent.change(field(), { target: { value: "first" } });
    fireEvent.keyDown(field(), { key: "Enter", shiftKey: true });
    expect(asked(calls)).toHaveLength(0);
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
    await renderTalk(transport);
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
    await renderTalk(transport);
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
    await renderTalk(transport);

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

  it("starts no conversation until the first message, so opening Talk records nothing", async () => {
    const { transport, calls } = routedTransport({});
    await renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Start a new conversation" }));
    expect(asked(calls)).toHaveLength(0);
  });

  it("gives the message back unsent when the history is unavailable, and answers nothing unkept", async () => {
    const { transport, calls } = routedTransport({ "conversation.start": failure("unavailable") });
    await renderTalk(transport);
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
      "conversation.read": [
        async () => ok({ conversation: {}, turns: [archivedTurn({ content: "why?", state: "failed", reason: "the provider failed", sendId: sendIdOf(calls) })] }),
        async () => ok({ conversation: {}, turns: [archivedTurn({ content: "why?", reply: "At last.", attempts: 2, sendId: sendIdOf(calls) })] }),
      ],
    });
    await renderTalk(transport);
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
    await renderTalk(transport);
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
    await renderTalk(transport);
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
    await renderTalk(transport);
    fireEvent.change(field(), { target: { value: "wait" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    await screen.findByText("This message is still on its way.");
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    await screen.findByText("Done now.");
    expect(sent(calls)).toHaveLength(1);
  });

  it("speaks Portuguese as Ningo, in the first person", async () => {
    const { transport } = routedTransport({});
    await renderTalk(transport, "pt");

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

const kept = (n: number, turns: number, kind: "general" | "reading" = "general", title = "") =>
  ({ id: archiveId(n), kind, readingId: kind === "reading" ? "item-1" : "", title, updatedAt: new Date().toISOString(), revision: 1, turns });
const listed = (...conversations: ReturnType<typeof kept>[]) => ok({ conversations, next: "" });
const readOf = (n: number, ...turns: ReturnType<typeof archivedTurn>[]) => ok({ conversation: kept(n, turns.length), turns });

describe("the kept conversations", () => {
  afterEach(() => vi.useRealTimers());

  it("continues the latest general conversation when the desktop starts", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(9, 2, "reading"), kept(3, 1)),
      "conversation.read": readOf(3, archivedTurn({ content: "Earlier question", reply: "Earlier answer.", sendId: "send-old1" })),
      "message.created": answering("Continued."),
    });
    await renderTalk(transport);

    screen.getByText("Earlier answer.");
    expect(screen.queryByText("I've read more than you'd guess. Ask.")).toBeNull();
    fireEvent.change(field(), { target: { value: "and now?" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Continued.");
    expect(sent(calls)[0]?.conversation_id).toBe(archiveId(3));
    expect(calls.filter((call) => call.type === "conversation.start")).toHaveLength(0);
    expect(calls.find((call) => call.type === "conversation.read")?.payload).toEqual({ conversation_id: archiveId(3) });
  });

  it("shows a turn a restart cut off and retries it as the same turn", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": [
        readOf(3, archivedTurn({ content: "Cut off", state: "interrupted", sendId: "send-cut1" })),
        readOf(3, archivedTurn({ content: "Cut off", reply: "Written now.", sendId: "send-cut1", attempts: 2 })),
      ],
      "message.created": async () => ok({ reply: "Written now.", replayed: false, turn: archivedTurn({ content: "Cut off", reply: "Written now.", sendId: "send-cut1", attempts: 2 }) }),
    });
    await renderTalk(transport);

    screen.getByText("The orchestrator stopped before this reply; nobody knows whether it was written.");
    fireEvent.click(screen.getByRole("button", { name: "Retry the last message" }));
    await screen.findByText("Written now.");
    expect(sent(calls)).toEqual([{ conversation_id: archiveId(3), retry_turn_id: archiveId(0x101) }]);
    expect(screen.getAllByText("Cut off")).toHaveLength(1);
  });

  it("opens a new conversation when nothing was kept, or the list cannot be read", async () => {
    for (const list of [listed(), failure("unavailable"), listed(kept(4, 0))]) {
      const { transport, calls } = routedTransport({ "conversation.list": list });
      const view = await renderTalk(transport);
      screen.getByText("I've read more than you'd guess. Ask.");
      expect(calls.filter((call) => call.type === "conversation.read")).toHaveLength(0);
      view.unmount();
    }
  });

  it("says when a kept conversation cannot be read, keeps it unsendable, and reads it again on request", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": [failure("unavailable"), readOf(3, archivedTurn({ content: "Back", reply: "Here it is." }))],
    });
    await renderTalk(transport);

    expect(field().disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("Here it is.");
    expect(field().disabled).toBe(false);
    expect(sent(calls)).toHaveLength(0);
  });

  it("lists the kept conversations and reopens the chosen one, dropping what was on its way", async () => {
    const late = deferred<TransportResult>();
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1, "general", "On justice"), kept(5, 2, "reading", "A reading")),
      "conversation.read": [
        readOf(3, archivedTurn({ content: "What is just?", reply: "Ask Thrasymachus.", sendId: "send-just" })),
        readOf(5, archivedTurn({ content: "Explain it", reply: "The reading argues for rules.", sendId: "send-read" })),
      ],
      "message.created": () => late.promise,
    });
    await renderTalk(transport);
    fireEvent.change(field(), { target: { value: "pending here" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Ningo is thinking…");

    fireEvent.click(screen.getByRole("button", { name: "Conversations" }));
    const current = await screen.findByRole("button", { name: "On justice" });
    expect(current.getAttribute("aria-current")).toBe("true");
    screen.getByText(/Reading discussion · 2 messages/);
    fireEvent.click(screen.getByRole("button", { name: "A reading" }));

    await screen.findByText("The reading argues for rules.");
    screen.getByText("A discussion about a reading, reopened from the archive.");
    expect(screen.queryByRole("button", { name: "A reading" })).toBeNull();
    await act(async () => late.resolve(archivedAnswer("Late, for the other one.", { content: "pending here" })));
    expect(screen.queryByText("Late, for the other one.")).toBeNull();
    expect(calls.filter((call) => call.type === "conversation.read").map((call) => call.payload)).toEqual([
      { conversation_id: archiveId(3) }, { conversation_id: archiveId(5) },
    ]);
  });

  it("says so when no conversation was kept yet", async () => {
    const { transport } = routedTransport({ "conversation.list": listed() });
    await renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Conversations" }));
    await screen.findByText("No conversations kept yet.");
  });

  it("looks again at a reopened message still on its way until its reply is kept, without sending it", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": [
        readOf(3, archivedTurn({ content: "Slow", state: "pending", sendId: "send-slow" })),
        readOf(3, archivedTurn({ content: "Slow", state: "pending", sendId: "send-slow" })),
        readOf(3, archivedTurn({ content: "Slow", reply: "Finally.", sendId: "send-slow" })),
      ],
    });
    await renderTalk(transport);
    screen.getByText("This message is still on its way.");

    await act(async () => { await vi.advanceTimersByTimeAsync(3_000); });
    screen.getByText("This message is still on its way.");
    await act(async () => { await vi.advanceTimersByTimeAsync(3_000); });
    await screen.findByText("Finally.");
    expect(calls.filter((call) => call.type === "conversation.read")).toHaveLength(3);
    await act(async () => { await vi.advanceTimersByTimeAsync(9_000); });
    expect(calls.filter((call) => call.type === "conversation.read")).toHaveLength(3);
    expect(sent(calls)).toHaveLength(0);
  });
});

describe("a conversation continued elsewhere", () => {
  it("shows the turns another interface added, after a send and when the window comes back", async () => {
    const phone = archivedTurn({ seq: 2, content: "From the phone", reply: "Answered on the phone.", sendId: "wa-phone" });
    let archive: ReturnType<typeof archivedTurn>[] = [archivedTurn({ content: "Earlier", reply: "Earlier answer.", sendId: "send-old1" })];
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": async () => ok({ conversation: kept(3, archive.length), turns: structuredClone(archive) }),
      "message.created": async (payload) => {
        const answer = archivedTurn({ seq: archive.length + 1, content: String(payload?.content), reply: "Desktop answer.", sendId: String(payload?.send_id) });
        archive = [...archive, answer];
        return ok({ reply: "Desktop answer.", replayed: false, turn: answer });
      },
    });
    await renderTalk(transport);
    screen.getByText("Earlier answer.");

    archive = [...archive, phone];
    act(() => { window.dispatchEvent(new Event("focus")); });
    await screen.findByText("Answered on the phone.");

    fireEvent.change(field(), { target: { value: "From the desktop" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Desktop answer.");
    const shown = [...document.querySelectorAll(".assistant-turns li p")].map((each) => each.textContent);
    expect(shown).toEqual(["Earlier", "Earlier answer.", "From the phone", "Answered on the phone.", "From the desktop", "Desktop answer."]);
    expect(sent(calls)).toHaveLength(1);
  });
});

describe("a turn without a reply, in Portuguese", () => {
  it("says the archive's own reasons in the screen's language and keeps any other as recorded", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 2)),
      "conversation.read": readOf(3,
        archivedTurn({ seq: 1, content: "Primeira", state: "failed", reason: "the model returned no reply", sendId: "send-one1" }),
        archivedTurn({ seq: 2, content: "Segunda", state: "failed", reason: "provider says no", sendId: "send-two2" }),
      ),
    });
    await renderTalk(transport, "pt");
    screen.getByText("Sem resposta: o modelo não devolveu resposta");
    screen.getByText("Sem resposta: provider says no");
  });
});
