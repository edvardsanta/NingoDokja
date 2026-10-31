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

  it("reads earlier turns on request and keeps them above the latest page", async () => {
    const turn = (seq: number) => archivedTurn({ seq, content: `Question ${seq}`, reply: `Answer ${seq}`, sendId: `send-${String(seq).padStart(4, "0")}` });
    const { transport, calls } = routedTransport({
      "conversation.list": ok({ conversations: [kept(3, 3)], next: "" }),
      "conversation.read": [readOf(3, turn(101), turn(102)), readOf(3, turn(1), turn(2)), readOf(3, turn(101), turn(102), turn(103))],
    });
    renderTalk(transport);
    await screen.findByText("Question 101");
    fireEvent.click(screen.getByRole("button", { name: "Earlier messages" }));
    await screen.findByText("Question 1");
    expect(calls.filter((call) => call.type === "conversation.read")[1]?.payload).toEqual({ conversation_id: archiveId(3), before_seq: 101 });
    expect(screen.queryByRole("button", { name: "Earlier messages" })).toBeNull();

    // Coming back to the window reads the latest page again: what was read stays above it, in order.
    fireEvent.focus(window);
    await screen.findByText("Question 103");
    const shown = [...document.querySelectorAll(".assistant-turns [data-role=user] p")].map((node) => node.textContent);
    expect(shown).toEqual(["Question 1", "Question 2", "Question 101", "Question 102", "Question 103"]);
  });

  it("says why earlier turns could not be read and keeps the conversation", async () => {
    const turn = archivedTurn({ seq: 5, content: "Still here", reply: "Yes", sendId: "send-0005" });
    const { transport } = routedTransport({
      "conversation.list": ok({ conversations: [kept(3, 1)], next: "" }),
      "conversation.read": [readOf(3, turn), failure("unavailable")],
    });
    renderTalk(transport);
    await screen.findByText("Still here");
    fireEvent.click(screen.getByRole("button", { name: "Earlier messages" }));
    await screen.findByRole("alert");
    screen.getByText("Still here");
    expect(screen.getByRole("button", { name: "Earlier messages" })).toBeTruthy();
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

describe("renaming and deleting kept conversations", () => {
  const openList = async () => {
    fireEvent.click(screen.getByRole("button", { name: "Conversations" }));
    await screen.findByRole("button", { name: "Rename On justice" });
  };

  it("renames a conversation in place, naming the version it saw", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1, "general", "On justice")),
      "conversation.read": readOf(3, archivedTurn({ content: "q", reply: "a" })),
      "conversation.rename": ok({ conversation: { ...kept(3, 1, "general", "Justice, again"), revision: 2 } }),
    });
    await renderTalk(transport);
    await openList();
    fireEvent.click(screen.getByRole("button", { name: "Rename On justice" }));
    const title = screen.getByRole("textbox", { name: "Title" }) as HTMLInputElement;
    expect(document.activeElement).toBe(title);
    fireEvent.change(title, { target: { value: "Justice, again" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await screen.findByRole("button", { name: "Justice, again" });
    expect(calls.find((call) => call.type === "conversation.rename")?.payload).toEqual({ conversation_id: archiveId(3), revision: 1, title: "Justice, again" });
  });

  it("leaves the field with Escape and keeps the drawer open", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1, "general", "On justice")),
      "conversation.read": readOf(3, archivedTurn({ content: "q", reply: "a" })),
    });
    await renderTalk(transport);
    await openList();
    fireEvent.click(screen.getByRole("button", { name: "Rename On justice" }));
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Title" }), { key: "Escape" });
    screen.getByRole("button", { name: "On justice" });
    expect(calls.filter((call) => call.type === "conversation.rename")).toHaveLength(0);
  });

  it("reads the list again when the conversation changed elsewhere, and says nothing was done", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": [listed(kept(3, 1, "general", "On justice")), listed(kept(3, 1, "general", "On justice")), listed({ ...kept(3, 2, "general", "On justice"), revision: 3 })],
      "conversation.read": readOf(3, archivedTurn({ content: "q", reply: "a" })),
      "conversation.delete": failure("orchestrator", "conversation changed; read it again"),
    });
    await renderTalk(transport);
    await openList();
    fireEvent.click(screen.getByRole("button", { name: "Delete On justice" }));
    await screen.findByText("That conversation changed elsewhere, so nothing was done; here it is as it is now.");
    screen.getByText(/^2 messages/);
    expect(calls.filter((call) => call.type === "conversation.list")).toHaveLength(3);
  });

  it("deletes a conversation and, when it was the one on screen, opens a new one", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": listed(kept(3, 1, "general", "On justice"), kept(4, 1, "general", "Other")),
      "conversation.read": readOf(3, archivedTurn({ content: "What is just?", reply: "Ask again." })),
      "conversation.delete": ok({ conversation_id: archiveId(3), deleted: true }),
    });
    await renderTalk(transport);
    screen.getByText("Ask again.");
    await openList();
    fireEvent.click(screen.getByRole("button", { name: "Delete On justice" }));

    await waitFor(() => expect(screen.queryByRole("button", { name: "On justice" })).toBeNull());
    screen.getByRole("button", { name: "Other" });
    expect(screen.queryByText("Ask again.")).toBeNull();
    screen.getByText("I've read more than you'd guess. Ask.");
    expect(calls.find((call) => call.type === "conversation.delete")?.payload).toEqual({ conversation_id: archiveId(3), revision: 1 });
  });

  it("keeps the conversation when the owner says no in the shell's window", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 1, "general", "On justice")),
      "conversation.read": readOf(3, archivedTurn({ content: "q", reply: "Still here." })),
      "conversation.delete": failure("cancelled", "the person did not confirm"),
    });
    await renderTalk(transport);
    await openList();
    fireEvent.click(screen.getByRole("button", { name: "Delete On justice" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Delete On justice" }).hasAttribute("disabled")).toBe(false));
    screen.getByRole("button", { name: "On justice" });
    screen.getByText("Still here.");
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("the conversation's layout", () => {
  const send = () => screen.getByRole("button", { name: "Send" });

  it("names each author once, before the text, so the owner's turn and Ningo's read apart", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": readOf(3, archivedTurn({ content: "A question", reply: "An answer.", sendId: "send-lay1" })),
    });
    await renderTalk(transport);

    const turns = [...document.querySelectorAll(".assistant-turns li[data-role]")];
    expect(turns.map((turn) => turn.getAttribute("data-role"))).toEqual(["user", "assistant"]);
    // The label comes first and the text is one paragraph, so the two registers can be styled apart.
    expect(turns.map((turn) => turn.firstElementChild?.tagName)).toEqual(["STRONG", "STRONG"]);
    expect(turns.map((turn) => turn.firstElementChild?.textContent)).toEqual(["You", "Ningo"]);
    expect(turns.map((turn) => turn.querySelectorAll("p").length)).toEqual([1, 1]);
  });

  it("keeps the header's two actions compact, so Send is the one main action in the view", async () => {
    const { transport } = routedTransport({});
    await renderTalk(transport);

    for (const name of ["Conversations", "Start a new conversation"]) {
      const action = screen.getByRole("button", { name });
      expect(action.classList.contains("compact"), name).toBe(true);
      expect(action.classList.contains("primary"), name).toBe(false);
    }
    expect(screen.getAllByRole("button").filter((button) => button.classList.contains("primary")).map((button) => button.textContent)).toEqual(["Send"]);
  });

  it("starts the field at one line and marks Send busy only while a reply is on its way", async () => {
    const late = deferred<TransportResult>();
    const { transport } = routedTransport({ "conversation.start": startedConversation(), "message.created": () => late.promise });
    await renderTalk(transport);
    expect(field().rows).toBe(1);
    expect(send().hasAttribute("aria-busy")).toBe(false);

    fireEvent.change(field(), { target: { value: "question" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByText("Ningo is thinking…");
    expect(send().getAttribute("aria-busy")).toBe("true");

    await act(async () => late.resolve(archivedAnswer("Done.", { content: "question" })));
    await screen.findByText("Done.");
    expect(send().hasAttribute("aria-busy")).toBe(false);
  });
});

describe("a send waiting for its turn", () => {
  it("stops a reply on its way and tells the turn's outcome, not the stream's error, then retries it", async () => {
    const reply = deferred<TransportResult>();
    const stoppedTurn = (calls: Parameters<typeof sendIdOf>[0]) => archivedTurn({ content: "why?", state: "failed", reason: "the owner stopped this reply", sendId: sendIdOf(calls) });
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": async () => reply.promise,
      "conversation.stop": async () => { reply.resolve(failure("orchestrator", "the owner stopped this reply")); return ok({ stopping: true }); },
      "conversation.read": async () => ok({ conversation: {}, turns: [stoppedTurn(calls)] }),
    });
    await renderTalk(transport);
    expect(screen.queryByRole("button", { name: "Stop the reply" })).toBeNull();
    fireEvent.change(field(), { target: { value: "why?" } });
    fireEvent.keyDown(field(), { key: "Enter" });

    fireEvent.click(await screen.findByRole("button", { name: "Stop the reply" }));
    await screen.findByRole("button", { name: "Retry the last message" });
    expect(calls.find((call) => call.type === "conversation.stop")?.payload).toEqual({ conversation_id: archiveId(1), send_id: sendIdOf(calls) });
    expect(screen.queryByRole("button", { name: "Stop the reply" })).toBeNull();
    // The stream's error is not said on top of the turn's own outcome, and only one send was made.
    expect(screen.getAllByText(/you stopped this reply/)).toHaveLength(1);
    expect(sent(calls)).toHaveLength(1);
  });

  it("stops a send another interface left on its way, and reads how it ended", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": ok({ conversations: [{ id: archiveId(1), kind: "general", reading_id: "", title: "Elsewhere", updated_at: "2026-01-01T00:00:00Z", revision: 1, turns: 1 }], next: "" }),
      "conversation.read": [
        ok({ conversation: {}, turns: [archivedTurn({ content: "why?", state: "pending", sendId: "send-elsewhere" })] }),
        ok({ conversation: {}, turns: [archivedTurn({ content: "why?", state: "failed", reason: "the owner stopped this reply", sendId: "send-elsewhere" })] }),
      ],
      "conversation.stop": ok({ stopping: true }),
    });
    render(<I18nProvider locale="en"><AssistantCard transport={transport} /></I18nProvider>);

    fireEvent.click(await screen.findByRole("button", { name: "Stop the reply" }));
    await screen.findByRole("button", { name: "Retry the last message" });
    expect(calls.find((call) => call.type === "conversation.stop")?.payload).toEqual({ conversation_id: archiveId(1), send_id: "send-elsewhere" });
    await waitFor(() => expect(document.activeElement).toBe(field()));
  });

  it("says when a reply could not be stopped, for as long as it is still on its way, in Portuguese too", async () => {
    const reply = deferred<TransportResult>();
    const { transport, calls } = routedTransport({
      "conversation.start": startedConversation(),
      "message.created": async () => reply.promise,
      "conversation.stop": failure("orchestrator", "conversation changed; read it again"),
      "conversation.read": async () => ok({ conversation: {}, turns: [archivedTurn({ content: "oi", reply: "Chegou.", sendId: sendIdOf(calls) })] }),
    });
    await renderTalk(transport, "pt");
    fireEvent.change(screen.getByRole("textbox", { name: "Sua mensagem" }), { target: { value: "oi" } });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Sua mensagem" }), { key: "Enter" });

    fireEvent.click(await screen.findByRole("button", { name: "Parar a resposta" }));
    await screen.findByText("Não consegui parar a resposta. Ela ainda pode chegar.");
    expect(screen.queryByText("conversation changed; read it again")).toBeNull();

    reply.resolve(ok({ reply: "Chegou.", replayed: false, turn: archivedTurn({ content: "oi", reply: "Chegou.", sendId: sendIdOf(calls) }) }));
    await screen.findByText("Chegou.");
    expect(screen.queryByText("Não consegui parar a resposta. Ela ainda pode chegar.")).toBeNull();
  });

  it("says it is waiting while the orchestrator has it queued, then goes back to thinking, in Portuguese too", async () => {
    let listener: ((preview: ChatPreview) => void) | undefined;
    const pending = deferred<TransportResult>();
    let conversationId = "";
    let payload: Record<string, unknown> | undefined;
    const transport: Transport = {
      request: (type, sent) => {
        if (type === "conversation.start") return Promise.resolve(startedConversation());
        if (type !== "message.created") return Promise.resolve(failure("unavailable"));
        conversationId = String(sent?.conversation_id); payload = sent;
        return pending.promise;
      },
      preview: async () => ({ ok: false, error: { code: "unavailable", message: "none" } }),
      onChatPreview: (each) => { listener = each; return () => { listener = undefined; }; },
    };
    await renderTalk(transport, "pt");
    fireEvent.change(screen.getByRole("textbox", { name: "Sua mensagem" }), { target: { value: "oi" } });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Sua mensagem" }), { key: "Enter" });
    await screen.findByText("Ningo está pensando…");
    await waitFor(() => expect(listener).toBeDefined());

    act(() => listener?.({ conversationId: "another", text: "", queued: true }));
    expect(screen.queryByText("Esperando a resposta anterior desta conversa…")).toBeNull();
    act(() => listener?.({ conversationId, text: "", queued: true }));
    screen.getByText("Esperando a resposta anterior desta conversa…");
    act(() => listener?.({ conversationId, text: "", queued: false }));
    screen.getByText("Ningo está pensando…");
    await act(async () => pending.resolve(archivedAnswer("Respondido.", payload)));
    await screen.findByText("Respondido.");
  });
});

describe("following the conversation", () => {
  const scrolled = (element: HTMLElement, atEnd: boolean) => {
    Object.defineProperty(element, "scrollHeight", { configurable: true, value: 2000 });
    Object.defineProperty(element, "clientHeight", { configurable: true, value: 500 });
    Object.defineProperty(element, "scrollTop", { configurable: true, value: atEnd ? 1500 : 200 });
    fireEvent.scroll(element);
  };

  it("does not take the page from under someone reading further up, and follows again when they send", async () => {
    let listener: ((preview: ChatPreview) => void) | undefined;
    const pending = [deferred<TransportResult>(), deferred<TransportResult>()];
    let sends = 0;
    let conversationId = "";
    const transport: Transport = {
      request: (type, payload) => {
        if (type === "conversation.start") return Promise.resolve(startedConversation());
        if (type !== "message.created") return Promise.resolve(failure("unavailable"));
        conversationId = String(payload?.conversation_id);
        return pending[sends++]!.promise;
      },
      preview: async () => ({ ok: false, error: { code: "unavailable", message: "none" } }),
      onChatPreview: (each) => { listener = each; return () => { listener = undefined; }; },
    };
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    await renderTalk(transport);
    const body = field().closest(".wp")?.querySelector(".wp-body") as HTMLElement;
    fireEvent.change(field(), { target: { value: "first" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await waitFor(() => expect(listener).toBeDefined());

    scrolled(body, false);
    scroll.mockClear();
    act(() => listener?.({ conversationId, text: "Writing" }));
    screen.getByText("Writing");
    expect(scroll).not.toHaveBeenCalled();

    scrolled(body, true);
    act(() => listener?.({ conversationId, text: "Writing more" }));
    expect(scroll).toHaveBeenCalled();

    scrolled(body, false);
    await act(async () => pending[0]!.resolve(archivedAnswer("Done.", { content: "first" })));
    await screen.findByText("Done.");
    scroll.mockClear();
    fireEvent.change(field(), { target: { value: "second" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await waitFor(() => expect(scroll).toHaveBeenCalled());
  });
});

describe("who answered", () => {
  it("shows the model the runtime reported, and nothing when it reported none", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 2)),
      "conversation.read": readOf(3,
        archivedTurn({ seq: 1, content: "One", reply: "Reported.", sendId: "send-one1", provider: "ollama", model: "example/small" }),
        archivedTurn({ seq: 2, content: "Two", reply: "Unreported.", sendId: "send-two2" }),
      ),
    });
    await renderTalk(transport, "pt");
    screen.getByText("Respondido por ollama / example/small");
    expect(document.querySelectorAll(".assistant-by")).toHaveLength(1);
  });
});

describe("the sources of a reply", () => {
  const FOOTER = "📚 [1] A very long title about the quiet economics of attention in the age of endless feeds · [2] Notes · on reading slowly";
  const CITED = `The library holds two texts on this[1], and the second[2] disagrees.\n\n${FOOTER}`;
  const list = () => screen.getByRole("list", { name: "Sources of this reply" });

  it("lists each document under the reply instead of the text line, and ties the citations to it", async () => {
    const { transport } = routedTransport({ "conversation.start": startedConversation(), "message.created": answering(CITED) });
    await renderTalk(transport);

    fireEvent.change(field(), { target: { value: "what does the library say?" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await screen.findByRole("list", { name: "Sources of this reply" });

    const entries = Array.from(list().querySelectorAll("li"));
    expect(entries.map((entry) => entry.textContent)).toEqual([
      "[1]A very long title about the quiet economics of attention in the age of endless feeds",
      "[2]Notes · on reading slowly",
    ]);
    // A cut title is still whole for the pointer and for a reader of the screen.
    expect(entries[0]?.getAttribute("title")).toBe("A very long title about the quiet economics of attention in the age of endless feeds");
    const reply = list().closest("li")!;
    expect(reply.textContent).not.toContain("📚");
    expect(reply.querySelector("p")?.textContent).toBe("The library holds two texts on this[1], and the second[2] disagrees.");
    const marks = Array.from(reply.querySelectorAll("p .cite"));
    expect(marks.map((mark) => mark.textContent)).toEqual(["[1]", "[2]"]);
    expect(marks[1]?.getAttribute("title")).toBe("Notes · on reading slowly");
    // The list sits inside the same turn, before who answered.
    expect(list().parentElement).toBe(reply);
  });

  it("shows the same list when the conversation is reopened from the archive", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": readOf(3, archivedTurn({ content: "Earlier question", reply: `${CITED}\n\nKept: you like tea.`, sendId: "send-old1", provider: "demo", model: "small" })),
    });
    await renderTalk(transport);

    expect(list().querySelectorAll("li")).toHaveLength(2);
    expect(list().closest("li")!.textContent).toContain("Kept: you like tea.");
    expect(list().closest("li")!.textContent).not.toContain("📚");
    expect(list().nextElementSibling?.textContent).toBe("Answered by demo / small");
  });

  it("leaves a reply with no sources as it is", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": readOf(3, archivedTurn({ content: "Q", reply: "No documents, though the array a[0] and [1] stay.", sendId: "send-old1" })),
    });
    await renderTalk(transport);

    screen.getByText("No documents, though the array a[0] and [1] stay.");
    expect(screen.queryByRole("list", { name: "Sources of this reply" })).toBeNull();
    expect(document.querySelector(".cite")).toBeNull();
  });

  it("renders a document's title as text and says so in Portuguese", async () => {
    const { transport } = routedTransport({
      "conversation.list": listed(kept(3, 1)),
      "conversation.read": readOf(3, archivedTurn({ content: "Q", reply: "Sim[1].\n\n📚 [1] <img src=x alt=bad>", sendId: "send-old1" })),
    });
    await renderTalk(transport, "pt");

    const sources = screen.getByRole("list", { name: "Fontes desta resposta" });
    expect(sources.textContent).toBe("[1]<img src=x alt=bad>");
    expect(sources.querySelector("img")).toBeNull();
  });

  it("keeps the reply as plain text while it is written and shows the documents when it lands", async () => {
    let listener: ((preview: ChatPreview) => void) | undefined;
    let answer: (value: TransportResult) => void = () => {};
    let sentPayload: Record<string, unknown> | undefined;
    const transport: Transport = {
      request: (type, payload) => {
        if (type === "conversation.start") return Promise.resolve(startedConversation());
        if (type !== "message.created") return Promise.resolve(ok({}));
        sentPayload = payload;
        return new Promise((resolve) => (answer = resolve));
      },
      preview: async () => ({ ok: false, error: { code: "unavailable", message: "none" } }),
      onChatPreview: (each) => {
        listener = each;
        return () => { listener = undefined; };
      },
    };
    await renderTalk(transport);
    fireEvent.change(field(), { target: { value: "ask" } });
    fireEvent.keyDown(field(), { key: "Enter" });
    await waitFor(() => expect(listener).toBeDefined());

    act(() => listener?.({ conversationId: archiveId(1), text: "The library holds two texts on this[1]" }));
    screen.getByText("The library holds two texts on this[1]");
    expect(screen.queryByRole("list", { name: "Sources of this reply" })).toBeNull();
    expect(document.querySelector(".cite")).toBeNull();

    act(() => answer(archivedAnswer(CITED, sentPayload)));
    await screen.findByRole("list", { name: "Sources of this reply" });
    expect(document.querySelectorAll(".cite")).toHaveLength(2);
  });
});
