import { fireEvent, render, screen } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, expect, it } from "vitest";

import type { CardEntry } from "../../src/renderer/cards/registry.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { PulseProvider, usePulse, type Tones } from "../../src/renderer/pulse.js";
import { Sidebar } from "../../src/renderer/sidebar.js";
import type { Transport } from "../../src/shared/transport.js";

const NO_TRANSPORT = {} as Transport;

// Three stand-in cards, so the sidebar is tested on its own. Each records when it is mounted and
// keeps a count of its own, which shows whether a card survives while another workspace is open.
function entries(mounted: string[]): CardEntry[] {
  const card = (name: string) =>
    function Stand() {
      const [clicks, setClicks] = useState(0);
      mounted.push(name);
      return (
        <div>
          <p>card {name}</p>
          <button type="button" onClick={() => setClicks((count) => count + 1)}>
            count {name}: {clicks}
          </button>
          <input aria-label={`field ${name}`} />
        </div>
      );
    };
  return [
    { kind: "a", labelId: "kind_digest", Card: card("a") },
    { kind: "b", labelId: "kind_memory", Card: card("b") },
    { kind: "c", labelId: "kind_knowledge", Card: card("c") },
  ];
}

function renderTabs(mounted: string[] = [], start?: string) {
  render(
    <I18nProvider locale="en">
      <PulseProvider>
        <Sidebar entries={entries(mounted)} transport={NO_TRANSPORT} start={start} />
      </PulseProvider>
    </I18nProvider>,
  );
  return mounted;
}

const tab = (name: string) => screen.getByRole("tab", { name });
const selected = () =>
  screen
    .getAllByRole("tab")
    .filter((each) => each.getAttribute("aria-selected") === "true")
    .map((each) => each.textContent);

describe("the sidebar", () => {
  it("opens the first workspace and mounts only its card", () => {
    const mounted = renderTabs();

    const list = screen.getByRole("tablist", { name: "Workspaces" });
    expect(list.getAttribute("aria-orientation")).toBe("vertical");
    expect(screen.getAllByRole("tab").map((each) => each.textContent)).toEqual(["digest", "memory", "knowledge"]);
    expect(selected()).toEqual(["digest"]);
    screen.getByText("card a");
    expect(screen.queryByText("card b")).toBeNull();
    expect(new Set(mounted)).toEqual(new Set(["a"]));
  });

  it("mounts a card the first time its workspace is chosen", () => {
    const mounted = renderTabs();
    fireEvent.click(tab("memory"));

    expect(selected()).toEqual(["memory"]);
    screen.getByText("card b");
    expect(new Set(mounted)).toEqual(new Set(["a", "b"]));
  });

  it("keeps a card, with what it showed, while another workspace is open", () => {
    renderTabs();
    fireEvent.click(screen.getByRole("button", { name: "count a: 0" }));
    fireEvent.click(tab("memory"));
    fireEvent.click(tab("digest"));

    screen.getByRole("button", { name: "count a: 1" });
    // every tab has its panel; the open one is the only one exposed, and the card of a tab that was
    // opened stays inside its hidden panel
    expect(screen.getAllByRole("tabpanel", { hidden: true })).toHaveLength(3);
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    expect(document.body.textContent).toContain("card b");
    fireEvent.click(tab("memory"));
    fireEvent.click(screen.getByRole("button", { name: "count b: 0" }));
    fireEvent.click(tab("digest"));
    fireEvent.click(tab("memory"));
    screen.getByRole("button", { name: "count b: 1" });
  });

  it("hides the card of a workspace that is not open from the page and from screen readers", () => {
    renderTabs();
    fireEvent.click(tab("memory"));

    const [first, second] = screen.getAllByRole("tabpanel", { hidden: true });
    expect(first?.hasAttribute("hidden")).toBe(true);
    expect(second?.hasAttribute("hidden")).toBe(false);
  });

  it("ties each entry to its panel", () => {
    renderTabs();
    fireEvent.click(tab("memory"));

    const panel = screen.getByRole("tabpanel");
    expect(tab("memory").getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.getAttribute("aria-labelledby")).toBe(tab("memory").id);
  });

  it("lets only the open entry take the Tab key", () => {
    renderTabs();

    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([0, -1, -1]);
    fireEvent.click(tab("knowledge"));
    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([-1, -1, 0]);
  });

  describe("with the keyboard", () => {
    const press = (key: string) => fireEvent.keyDown(document.activeElement ?? document.body, { key });

    it("moves with the up and down arrows, wrapping at both ends, and focuses the entry they open", () => {
      renderTabs();
      tab("digest").focus();

      press("ArrowDown");
      expect(selected()).toEqual(["memory"]);
      expect(document.activeElement).toBe(tab("memory"));
      press("ArrowDown");
      press("ArrowDown");
      expect(selected()).toEqual(["digest"]);
      press("ArrowUp");
      expect(selected()).toEqual(["knowledge"]);
      expect(document.activeElement).toBe(tab("knowledge"));
    });

    it("jumps to the first and last entry with Home and End", () => {
      renderTabs();
      tab("digest").focus();

      press("End");
      expect(selected()).toEqual(["knowledge"]);
      press("Home");
      expect(selected()).toEqual(["digest"]);
    });

    it("keeps the page from scrolling on the keys it uses, and leaves the other keys alone", () => {
      renderTabs();
      tab("digest").focus();

      // fireEvent answers false when the event was cancelled
      for (const key of ["ArrowDown", "ArrowUp", "Home", "End"]) {
        expect(fireEvent.keyDown(document.activeElement ?? document.body, { key }), key).toBe(false);
      }
      fireEvent.click(tab("memory"));
      for (const key of ["Tab", "Enter", "a", "ArrowRight", "constructor"]) {
        expect(fireEvent.keyDown(tab("memory"), { key }), key).toBe(true);
      }
      expect(selected()).toEqual(["memory"]);
    });

    it("opens the workspaces with Ctrl and a digit, in order", () => {
      renderTabs();

      fireEvent.keyDown(document.body, { key: "2", ctrlKey: true });
      expect(selected()).toEqual(["memory"]);
      expect(document.activeElement).toBe(tab("memory"));
      fireEvent.keyDown(document.body, { key: "3", ctrlKey: true });
      expect(selected()).toEqual(["knowledge"]);
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
      expect(selected()).toEqual(["digest"]);
    });

    it("leaves alone a bare digit, a digit with no workspace, a zero, and other modifiers", () => {
      renderTabs();
      fireEvent.click(tab("memory"));

      fireEvent.keyDown(document.body, { key: "1" });
      fireEvent.keyDown(document.body, { key: "9", ctrlKey: true });
      fireEvent.keyDown(document.body, { key: "0", ctrlKey: true });
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true, altKey: true });
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true, shiftKey: true });
      fireEvent.keyDown(document.body, { key: "1", metaKey: true });
      expect(selected()).toEqual(["memory"]);
    });

    it("leaves the workspaces alone while a dialog is open over them", () => {
      renderTabs();
      fireEvent.click(tab("memory"));
      const dialog = document.createElement("div");
      dialog.setAttribute("role", "dialog");
      dialog.setAttribute("aria-modal", "true");
      document.body.append(dialog);
      try {
        fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
        expect(selected()).toEqual(["memory"]);
      } finally {
        dialog.remove();
      }
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
      expect(selected()).toEqual(["digest"]);
    });

    it("types a bare digit into a field, and still opens a workspace on Ctrl and a digit from it", () => {
      renderTabs();
      fireEvent.click(tab("memory"));

      fireEvent.keyDown(screen.getByLabelText("field b"), { key: "3" });
      expect(selected()).toEqual(["memory"]);
      expect(fireEvent.keyDown(screen.getByLabelText("field b"), { key: "3", ctrlKey: true })).toBe(false);
      expect(selected()).toEqual(["knowledge"]);
    });
  });

  it("opens on the workspace it is told to start on, and mounts only that one", () => {
    const mounted = renderTabs([], "c");

    expect(selected()).toEqual(["knowledge"]);
    expect(new Set(mounted)).toEqual(new Set(["c"]));
  });

  describe("the state of each workspace", () => {
    // Stands in for the health card: it reports the tones the status gave, as that card does.
    function Reporter({ tones }: { tones: Tones }) {
      const { reportTones } = usePulse();
      useEffect(() => reportTones(tones), [tones, reportTones]);
      return null;
    }

    const stand = () => <p>stand-in</p>;
    const withServices: CardEntry[] = [
      { kind: "a", labelId: "kind_knowledge", Card: stand, services: ["knowledge"] },
      { kind: "b", labelId: "nav_messages", Card: stand, services: ["discord", "whatsapp"] },
      { kind: "c", labelId: "kind_digest", Card: stand },
      { kind: "d", labelId: "nav_system", Card: stand, foot: true },
    ];

    const renderWith = (tones: Tones) =>
      render(
        <I18nProvider locale="en">
          <PulseProvider>
            <Reporter tones={tones} />
            <Sidebar entries={withServices} transport={NO_TRANSPORT} />
          </PulseProvider>
        </I18nProvider>,
      );

    const dotOf = (name: string) => tab(name).querySelector(".tab-dot")?.getAttribute("data-tone");

    it("shows the worst tone among the services behind each workspace, and no dot for one it does not know", () => {
      renderWith({ knowledge: "ok", discord: "ok", whatsapp: "problem", meme: "off" });

      expect(dotOf("knowledge")).toBe("ok");
      expect(dotOf("messages")).toBe("problem");
      expect(dotOf("digest")).toBeUndefined();
      expect(dotOf("system")).toBeUndefined();
    });

    it("shows no dot before any status arrives", () => {
      renderWith({});

      expect(document.querySelectorAll(".tab-dot")).toHaveLength(0);
      expect(document.querySelectorAll(".tab-count")).toHaveLength(0);
    });

    it("counts the services with a problem at the foot, and keeps the count out of the entry's name", () => {
      renderWith({ knowledge: "problem", whatsapp: "problem", meme: "unchecked" });

      expect(tab("system").querySelector(".tab-count")?.textContent).toBe("2");
      expect(tab("system").hasAttribute("data-foot")).toBe(true);
    });
  });
});
