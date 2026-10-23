import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useCallback } from "react";
import { describe, expect, it } from "vitest";

import { CardFrame } from "../../src/renderer/cards/card_frame.js";
import { useCard } from "../../src/renderer/cards/use_card.js";
import { WorkspacePanel } from "../../src/renderer/cards/workspace_panel.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { TransportResult } from "../../src/shared/transport.js";
import { deferred } from "./support.js";

// A section that counts its reads and answers when the test says so.
function Section({ name, reads, answer }: { name: string; reads: string[]; answer: () => Promise<TransportResult> }) {
  const load = useCallback(async () => {
    reads.push(name);
    const result = await answer();
    return result.ok ? { ok: true as const, data: String(result.result) } : result;
  }, [name, reads, answer]);
  const { state, reload } = useCard(load);
  return (
    <CardFrame title={name} state={state} onReload={reload}>
      {(text) => <p>{text}</p>}
    </CardFrame>
  );
}

const now = (value: string) => async (): Promise<TransportResult> => ({ ok: true, result: value });

function renderPanel(sections: { name: string; answer: () => Promise<TransportResult> }[]) {
  const reads: string[] = [];
  render(
    <I18nProvider locale="en">
      <WorkspacePanel trail={["Library", "Documents"]} actions={<button type="button">Add</button>}>
        {sections.map((section) => (
          <Section key={section.name} name={section.name} reads={reads} answer={section.answer} />
        ))}
      </WorkspacePanel>
    </I18nProvider>,
  );
  return reads;
}

describe("the workspace panel", () => {
  it("names where the owner is in its legend and labels itself with it", async () => {
    renderPanel([{ name: "Research base", answer: now("ready") }]);

    const panel = screen.getByRole("region", { name: "Library › Documents" });
    const steps = [...panel.querySelectorAll(".wp-legend span")].map((step) => step.textContent);
    expect(steps).toEqual(["Library", "Documents"]);
    expect(panel.querySelector('[aria-current="location"]')?.textContent).toBe("Documents");
    await screen.findByText("ready");
  });

  it("has one refresh for the whole workspace and no refresh per section", async () => {
    const reads = renderPanel([
      { name: "First", answer: now("one") },
      { name: "Second", answer: now("two") },
    ]);
    await screen.findByText("two");

    expect(screen.getAllByRole("button", { name: "Refresh" })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(reads.toSorted()).toEqual(["First", "First", "Second", "Second"]));
  });

  it("holds the refresh while any section is still reading", async () => {
    const gate = deferred<TransportResult>();
    renderPanel([
      { name: "Quick", answer: now("quick") },
      { name: "Slow", answer: () => gate.promise },
    ]);
    await screen.findByText("quick");

    const refresh = screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement;
    expect(refresh.disabled).toBe(true);
    gate.resolve({ ok: true, result: "slow" });
    await screen.findByText("slow");
    expect(refresh.disabled).toBe(false);
  });

  it("offers to try again inside the section that failed", async () => {
    const reads = renderPanel([
      { name: "Fine", answer: now("fine") },
      { name: "Broken", answer: async () => ({ ok: false, error: { code: "unavailable", message: "" } }) },
    ]);
    await screen.findByRole("alert");

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(reads.filter((name) => name === "Broken")).toHaveLength(2));
    expect(reads.filter((name) => name === "Fine")).toHaveLength(1);
  });

  it("shows no refresh when nothing in it can be read again", () => {
    render(
      <I18nProvider locale="en">
        <WorkspacePanel trail={["Talk"]} actions={<button type="button">Start a new conversation</button>}>
          <p>a conversation</p>
        </WorkspacePanel>
      </I18nProvider>,
    );

    expect(screen.queryByRole("button", { name: "Refresh" })).toBeNull();
    screen.getByRole("button", { name: "Start a new conversation" });
  });
});
