import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../../src/renderer/app.js";
import { AssistantCard } from "../../src/renderer/cards/assistant.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { ChatPreview, Transport, TransportResult } from "../../src/shared/transport.js";
import { failure, ok, routedTransport } from "./support.js";

const reply = (text: string) => ok({ reply: text, sessionNotice: "" });

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

describe("the conversation", () => {
  it("opens with Ningo's line, then a plain next step, and its message docked at the foot", () => {
    const { transport } = routedTransport({});
    renderTalk(transport);

    const voice = screen.getByText("I've read more than you'd guess. Ask.");
    expect(voice.className).toBe("voice");
    expect(voice.nextElementSibling?.textContent).toBe("Ask anything, or start from one of these and finish the thought.");
    expect(field().closest(".wp-dock")).not.toBeNull();
    expect(field().placeholder).toBe("Ask Ningo…");
  });

  it("fills the message from a beginning and sends nothing until the owner does", () => {
    const { transport, calls } = routedTransport({ "message.created": reply("Then argue.") });
    renderTalk(transport);

    fireEvent.click(screen.getByRole("button", { name: "Argue against:" }));
    expect(field().value).toBe("Argue against: ");
    expect(document.activeElement).toBe(field());
    expect(calls).toHaveLength(0);
  });

  it("sends on Enter, breaks the line on Shift+Enter, and keeps one id for the whole conversation", async () => {
    const { transport, calls } = routedTransport({ "message.created": [reply("First answer."), reply("Second answer.")] });
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
    expect(payloads[0]?.conversation_id).toBe(payloads[1]?.conversation_id);
  });

  it("shows the reply as Ningo writes it, for this conversation only, and the reply replaces it", async () => {
    let listener: ((preview: ChatPreview) => void) | undefined;
    let answer: (value: TransportResult) => void = () => {};
    let conversationId = "";
    const transport: Transport = {
      request: (type, payload) => {
        if (type !== "message.created") return Promise.resolve(ok({}));
        conversationId = String(payload?.conversation_id);
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

    act(() => answer(reply("The answer.")));
    await screen.findByText("The answer.");
    expect(screen.queryByText("The ans")).toBeNull();
    expect(listener).toBeUndefined();
  });

  it("starts a new conversation with a new id and an empty transcript", async () => {
    const { transport, calls } = routedTransport({ "message.created": reply("Noted.") });
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
    expect(first?.conversation_id).not.toBe(second?.conversation_id);
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
