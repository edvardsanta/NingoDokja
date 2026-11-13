import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProfilesPanel } from "../../src/renderer/cards/profiles.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { ok, routedTransport } from "./support.js";

const READING = { summary: "Dry and short with Ana.", tone: "dry", topics: ["games"], patterns: ["teases"], exchanges: 12, generatedAt: "2026-11-13T12:00:00Z", provider: "local", model: "m1" };
const OWNER = { person: "owner", name: "Boss", realName: "", owner: true, turns: 9, voiceTurns: 2, answered: 7, medianReply: 180 };
const ANA = { person: "person:Ana", name: "Ana", realName: "Anabel", owner: false, turns: 20, voiceTurns: 0, answered: 12, medianReply: 220, profile: READING };
const RUI = { person: "person:Rui", name: "Rui", realName: "", owner: false, turns: 2, voiceTurns: 0, answered: 1, medianReply: 90 };
const LIST = (inference = true) => ok({ profiles: [OWNER, ANA, RUI], minExchanges: 3, inference });

function show(transport: Parameters<typeof ProfilesPanel>[0]["transport"]) {
  return render(<I18nProvider locale="en"><ProfilesPanel transport={transport} /></I18nProvider>);
}

describe("the profiles panel", () => {
  it("shows each person's counts and reading as text, with where the reading came from", async () => {
    const { transport } = routedTransport({ "conversation.profiles": LIST() });
    const { container } = show(transport);
    const ana = await screen.findByRole("region", { name: "Ana" });
    within(ana).getByText("Dry and short with Ana.");
    within(ana).getByText(/real name Anabel/);
    within(ana).getByText(/20 messages · 12 answered/);
    within(ana).getByText(/Inferred .* from 12 exchanges by m1\. A reading, not a fact\./);
    within(screen.getByRole("region", { name: "Boss" })).getByText("No profile yet.");
    expect(container.querySelector("img")).toBeNull();
  });

  it("explains why a person with too few answered exchanges cannot be inferred", async () => {
    const { transport } = routedTransport({ "conversation.profiles": LIST() });
    show(transport);
    const rui = await screen.findByRole("region", { name: "Rui" });
    within(rui).getByText("At least 3 answered exchanges are needed; there are 1.");
    expect((within(rui).getByRole("button", { name: "Infer the profile of Rui" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("infers one person on a press, with a long deadline, and reads the list again", async () => {
    const generated = { ...OWNER, profile: { ...READING, summary: "Direct with the owner." } };
    const { transport, calls } = routedTransport({
      "conversation.profiles": [LIST(), ok({ profiles: [generated, ANA, RUI], minExchanges: 3, inference: true })],
      "conversation.profile_generate": ok({ profile: generated }),
    });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Infer the profile of Boss" }));
    await act(async () => { await Promise.resolve(); });
    const call = calls.find((item) => item.type === "conversation.profile_generate");
    expect(call?.payload).toEqual({ person: "owner" });
    expect(call?.options?.timeoutMs).toBeGreaterThan(90_000);
    await screen.findByText("Direct with the owner.");
  });

  it("shows how much independent readings agreed, as figures that say what low means", async () => {
    const measured = { ...ANA, profile: { ...READING, stability: { samples: 3, tone: 0.67, summary: 0.52, topics: 0.4, patterns: 0.25 } } };
    const { transport } = routedTransport({ "conversation.profiles": ok({ profiles: [OWNER, measured, RUI], minExchanges: 3, inference: true }) });
    show(transport);
    const ana = await screen.findByRole("region", { name: "Ana" });
    within(ana).getByText(/across 3 readings of the same exchanges: tone 67% · summary 52% · topics 40% · patterns 25%\. Low means the model changed its mind\./);
    expect(within(screen.getByRole("region", { name: "Boss" })).queryByText(/Stability/)).toBeNull();
  });

  it("measures one person on a press, with a long deadline, and reads the list again", async () => {
    const measured = { ...OWNER, profile: { ...READING, stability: { samples: 3, tone: 1, summary: 0.8, topics: 0.5, patterns: 0.5 } } };
    const { transport, calls } = routedTransport({
      "conversation.profiles": [LIST(), ok({ profiles: [measured, ANA, RUI], minExchanges: 3, inference: true })],
      "conversation.profile_stability": ok({ profile: measured }),
    });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Measure the stability of the profile of Boss" }));
    await act(async () => { await Promise.resolve(); });
    const call = calls.find((item) => item.type === "conversation.profile_stability");
    expect(call?.payload).toEqual({ person: "owner" });
    expect(call?.options?.timeoutMs).toBeGreaterThan(90_000);
    await screen.findByText(/tone 100% · summary 80%/);
  });

  it("offers no measure to someone with too few answered exchanges", async () => {
    const { transport } = routedTransport({ "conversation.profiles": LIST() });
    show(transport);
    const rui = await screen.findByRole("region", { name: "Rui" });
    expect((within(rui).getByRole("button", { name: "Measure the stability of the profile of Rui" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("keeps the old reading and tells why when inferring fails", async () => {
    const { transport } = routedTransport({ "conversation.profiles": LIST() });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Infer the profile of Ana" }));
    await screen.findByRole("alert");
    screen.getByText("Dry and short with Ana.");
  });

  it("stays quiet when the owner cancels", async () => {
    const { transport } = routedTransport({
      "conversation.profiles": LIST(),
      "conversation.profile_generate": { ok: false, error: { code: "cancelled", message: "cancelled" } },
    });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Infer the profile of Ana" }));
    await act(async () => { await Promise.resolve(); });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("offers no inference when no model is available, and says so when nobody is listed", async () => {
    const { transport } = routedTransport({ "conversation.profiles": LIST(false) });
    const first = show(transport);
    const ana = await screen.findByRole("region", { name: "Ana" });
    expect((within(ana).getByRole("button", { name: "Infer the profile of Ana" }) as HTMLButtonElement).disabled).toBe(true);
    first.unmount();
    const empty = routedTransport({ "conversation.profiles": ok({ profiles: [], minExchanges: 3, inference: true }) });
    show(empty.transport);
    await screen.findByText("No one is listed yet.");
  });
});
