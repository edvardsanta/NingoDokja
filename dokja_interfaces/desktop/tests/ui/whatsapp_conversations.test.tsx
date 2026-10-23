import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { CommunicationsCard } from "../../src/renderer/cards/communications.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport } from "../../src/shared/transport.js";
import { ok, routedTransport } from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
        <CommunicationsCard transport={transport} />
    </I18nProvider>,
  );
}

const channels = ok({
  channels: [
    { id: "whatsapp:friend", name: "friend", retentionDays: 30 },
    { id: "123456789012345678", name: "reading-room" },
  ],
});

const page = (channelId: string) =>
  ok({
    channelId,
    before: "",
    messages: [
      { id: "1", author: "contact", bot: false, content: "are you coming?", timestamp: "2026-10-11T12:00:00.000Z", edited: false, replyTo: "", attachments: [] },
      { id: "2", author: "owner", bot: false, content: "yes", timestamp: "2026-10-11T12:01:00.000Z", edited: false, replyTo: "", attachments: [] },
      { id: "3", author: "bot", bot: true, content: "He will be there.", timestamp: "2026-10-11T12:02:00.000Z", edited: false, replyTo: "", attachments: [] },
    ],
  });

describe("a WhatsApp conversation in Messages", () => {
  it("is listed by its alias, shows who wrote in words, and says how long the text is kept", async () => {
    const { transport, calls } = routedTransport({
      "communications.channels": channels,
      "communications.history": page("whatsapp:friend"),
    });
    renderCard(transport);

    await screen.findByText("are you coming?");

    expect(screen.getByRole("button", { name: "WhatsApp · friend" }).getAttribute("aria-current")).toBe("true");
    expect(screen.getByText(/kept on this machine for 30 days/)).toBeTruthy();
    const messages = screen.getByRole("list", { name: "Conversation history" });
    expect(within(messages).getByText("Contact")).toBeTruthy();
    expect(within(messages).getByText("You")).toBeTruthy();
    expect(within(messages).getByText("Ningo")).toBeTruthy();
    expect(calls.find((call) => call.type === "communications.history")?.payload).toEqual({ channel_id: "whatsapp:friend", before: "" });
  });

  it("offers a text-only composer, with no memes to attach", async () => {
    const { transport } = routedTransport({
      "communications.channels": channels,
      "communications.history": page("whatsapp:friend"),
    });
    renderCard(transport);
    await screen.findByText("are you coming?");

    expect(screen.getByRole("textbox")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Load memes" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: /meme/i })).toBeNull();
    expect(screen.getByText(/go out from your own account, as you/)).toBeTruthy();
  });

  it("sends the text to the alias and nothing else", async () => {
    const { transport, calls } = routedTransport({
      "communications.channels": channels,
      "communications.history": page("whatsapp:friend"),
      "communications.send": ok({ sent: true, skipped: false }),
    });
    renderCard(transport);
    await screen.findByText("are you coming?");

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "  see you there  " } });
    fireEvent.click(screen.getByRole("button", { name: "Send as you" }));

    await screen.findByText("Message sent.");
    const sent = calls.filter((call) => call.type === "communications.send");
    expect(sent).toHaveLength(1);
    expect(sent[0]?.payload).toEqual({ channel_id: "whatsapp:friend", content: "see you there" });
  });

  it("says it in Portuguese", async () => {
    const { transport } = routedTransport({
      "communications.channels": channels,
      "communications.history": page("whatsapp:friend"),
    });
    renderCard(transport, "pt");

    await screen.findByText("are you coming?");

    expect(screen.getByText(/guardado nesta máquina por 30 dias/)).toBeTruthy();
    expect(screen.getByText("Contato")).toBeTruthy();
    expect(screen.getByText("Você")).toBeTruthy();
  });

  it("leaves a Discord channel as it was, with its composer", async () => {
    const { transport } = routedTransport({
      "communications.channels": ok({ channels: [{ id: "123456789012345678", name: "reading-room" }] }),
      "communications.history": page("123456789012345678"),
    });
    renderCard(transport);

    await waitFor(() => expect(screen.getByRole("textbox")).toBeTruthy());
    expect(screen.queryByText(/kept on this machine/)).toBeNull();
    expect(screen.getByRole("button", { name: "#reading-room · 123456789012345678" })).toBeTruthy();
  });
});
