import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ReplyContextLine } from "../../src/renderer/cards/reply_context.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";

const show = (context: Parameters<typeof ReplyContextLine>[0]["context"], locale: "en" | "pt" = "en") =>
  render(<I18nProvider locale={locale}><ReplyContextLine context={context} /></I18nProvider>);

describe("the line saying what an answer read", () => {
  it("says how much of the conversation it read and what became of the memory", () => {
    show({ liveMessages: 6, macro: "recalled", reason: "" });
    screen.getByText("Read: 6 earlier messages of this conversation · memory of earlier discussions: recalled");
  });

  it("says why the memory was not needed", () => {
    show({ liveMessages: 4, macro: "skipped", reason: "live_context_covers_it" });
    screen.getByText(/not needed, the conversation already covers it/);
  });

  it("words an unknown reason plainly instead of showing its code", () => {
    const { container } = show({ liveMessages: 2, macro: "skipped", reason: "some_future_reason" });
    expect(container.textContent).toContain("not needed");
    expect(container.textContent).not.toContain("some_future_reason");
  });

  it("says when the memory was needed but could not be asked, and when it had nothing", () => {
    const first = show({ liveMessages: 0, macro: "unavailable", reason: "" });
    screen.getByText(/needed but unavailable/);
    first.unmount();
    show({ liveMessages: 0, macro: "nothing_found", reason: "" });
    screen.getByText(/asked, nothing found/);
  });

  it("speaks Portuguese when the screen does", () => {
    show({ liveMessages: 3, macro: "skipped", reason: "short_follow_up" }, "pt");
    screen.getByText("Leu: 3 mensagens anteriores desta conversa · memória de discussões anteriores: não precisou, pergunta curta de continuação");
  });
});
