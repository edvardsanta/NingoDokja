import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProposalsCard } from "../../src/renderer/cards/proposals.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { failure, ok, routedTransport } from "./support.js";

const link = {
  id: "proposal:web_article:web:abc",
  kind: "web_article",
  payload: { url: "https://example.com/a" },
  proposedBy: "sam <1>",
  sentBy: "sam <1>",
  source: { channelId: "c1", messageId: "m1", permalink: "https://example.com/ch/1/m1" },
  proposedAt: "2026-11-14T12:00:00Z",
  current: { origin: "owner", title: "The law" },
};

const tag = {
  id: "proposal:meme_hashtag:meme-hashtag:x",
  kind: "meme_hashtag",
  payload: { tag: "Games", url: "https://media.example/meme.jpg" },
  proposedBy: "draco <2>",
  sentBy: "tiago <3>",
  source: { channelId: "memes", messageId: "m2", permalink: "https://example.com/ch/2/m2" },
  proposedAt: "2026-11-14T13:00:00Z",
};

const view = (transport: Transport) => <I18nProvider locale="en"><ProposalsCard transport={transport} /></I18nProvider>;

describe("proposals", () => {
  it("lists what is pending with its trace and empties after an approval", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.proposals.list": [ok({ proposals: [link] }), ok({ proposals: [] })],
      "knowledge.proposal.approve": ok({ proposalId: link.id, approved: true, stored: 1 }),
    });
    render(view(transport));
    await screen.findByText("https://example.com/a");
    const item = screen.getByLabelText("Link");
    expect(item.textContent).toContain("proposed by sam <1>");
    expect(item.textContent).toContain("current: The law (yours)");
    expect(within(item).getByRole("link", { name: "open the message" }).getAttribute("href")).toBe("https://example.com/ch/1/m1");
    expect(screen.queryByRole("textbox")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await screen.findByText("Approved and stored.");
    await screen.findByText("Nothing is waiting for a decision.");
    const approve = calls.find((call) => call.type === "knowledge.proposal.approve");
    expect(approve?.payload).toEqual({ proposal_id: link.id });
  });

  it("keeps a refused approval pending and says the fixed reason", async () => {
    const { transport } = routedTransport({
      "knowledge.proposals.list": ok({ proposals: [link] }),
      "knowledge.proposal.approve": ok({ proposalId: link.id, approved: false, reason: "blocked" }),
    });
    render(view(transport));
    await screen.findByText("https://example.com/a");
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await screen.findByText("kept pending: blocked");
    // The list reloads; the proposal is still there, so the same canned reply serves again.
    await screen.findByText("https://example.com/a");
  });

  it("shows the tag edit only for tag proposals and sends it with the approval", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.proposals.list": [ok({ proposals: [tag] }), ok({ proposals: [] })],
      "knowledge.proposal.approve": ok({ proposalId: tag.id, approved: true, stored: 1 }),
    });
    render(view(transport));
    await screen.findByText("#Games");
    expect(screen.getByLabelText("Meme tag").textContent).toContain("tag by draco <2>, meme by tiago <3>");
    const edit = screen.getByRole("textbox", { name: "edit the tag before approving" });
    fireEvent.change(edit, { target: { value: "IagoGames" } });
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      const approve = calls.find((call) => call.type === "knowledge.proposal.approve");
      expect(approve?.payload).toEqual({ proposal_id: tag.id, edit: { tag: "IagoGames" } });
    });
  });

  it("says plainly when proposals are not configured", async () => {
    const { transport } = routedTransport({ "knowledge.proposals.list": failure("orchestrator", "knowledge proposals are not configured") });
    render(view(transport));
    await screen.findByText("The orchestrator answered with an error: knowledge proposals are not configured");
  });
});
