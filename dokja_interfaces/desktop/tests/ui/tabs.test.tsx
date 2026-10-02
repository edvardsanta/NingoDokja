import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import type { CardEntry } from "../../src/renderer/cards/registry.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { Tabs } from "../../src/renderer/tabs.js";
import type { Transport } from "../../src/shared/transport.js";

const NO_TRANSPORT = {} as Transport;

// Three stand-in cards, so the tabs are tested on their own. Each records when it is mounted and
// keeps a count of its own, which shows whether a card survives while another tab is open.
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
    { kind: "a", labelId: "kind_health", Card: card("a") },
    { kind: "b", labelId: "kind_memory", Card: card("b") },
    { kind: "c", labelId: "kind_knowledge", Card: card("c") },
  ];
}

function renderTabs(mounted: string[] = []) {
  render(
    <I18nProvider locale="en">
      <Tabs entries={entries(mounted)} transport={NO_TRANSPORT} />
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

describe("the tabs", () => {
  it("open the first tab and mount only its card", () => {
    const mounted = renderTabs();

    expect(screen.getByRole("tablist", { name: "Sections" })).toBeTruthy();
    expect(screen.getAllByRole("tab").map((each) => each.textContent)).toEqual(["health", "memory", "knowledge"]);
    expect(selected()).toEqual(["health"]);
    screen.getByText("card a");
    expect(screen.queryByText("card b")).toBeNull();
    expect(new Set(mounted)).toEqual(new Set(["a"]));
  });

  it("mount a card the first time its tab is chosen", () => {
    const mounted = renderTabs();
    fireEvent.click(tab("memory"));

    expect(selected()).toEqual(["memory"]);
    screen.getByText("card b");
    expect(new Set(mounted)).toEqual(new Set(["a", "b"]));
  });

  it("keep a card, with what it showed, while another tab is open", () => {
    renderTabs();
    fireEvent.click(screen.getByRole("button", { name: "count a: 0" }));
    fireEvent.click(tab("memory"));
    fireEvent.click(tab("health"));

    screen.getByRole("button", { name: "count a: 1" });
    // every tab has its panel; the open one is the only one exposed, and the card of a tab that was
    // opened stays inside its hidden panel
    expect(screen.getAllByRole("tabpanel", { hidden: true })).toHaveLength(3);
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    expect(document.body.textContent).toContain("card b");
    fireEvent.click(tab("memory"));
    fireEvent.click(screen.getByRole("button", { name: "count b: 0" }));
    fireEvent.click(tab("health"));
    fireEvent.click(tab("memory"));
    screen.getByRole("button", { name: "count b: 1" });
  });

  it("hide the card of a tab that is not open from the page and from screen readers", () => {
    renderTabs();
    fireEvent.click(tab("memory"));

    const [first, second] = screen.getAllByRole("tabpanel", { hidden: true });
    expect(first?.hasAttribute("hidden")).toBe(true);
    expect(second?.hasAttribute("hidden")).toBe(false);
  });

  it("tie each tab to its panel", () => {
    renderTabs();
    fireEvent.click(tab("memory"));

    const panel = screen.getByRole("tabpanel");
    expect(tab("memory").getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.getAttribute("aria-labelledby")).toBe(tab("memory").id);
  });

  it("let only the open tab take the Tab key", () => {
    renderTabs();

    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([0, -1, -1]);
    fireEvent.click(tab("knowledge"));
    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([-1, -1, 0]);
  });

  describe("with the keyboard", () => {
    const press = (key: string) => fireEvent.keyDown(document.activeElement ?? document.body, { key });

    it("move with the arrows, wrapping at both ends, and focus the tab they open", () => {
      renderTabs();
      tab("health").focus();

      press("ArrowRight");
      expect(selected()).toEqual(["memory"]);
      expect(document.activeElement).toBe(tab("memory"));
      press("ArrowRight");
      press("ArrowRight");
      expect(selected()).toEqual(["health"]);
      press("ArrowLeft");
      expect(selected()).toEqual(["knowledge"]);
      expect(document.activeElement).toBe(tab("knowledge"));
    });

    it("jump to the first and last tab with Home and End", () => {
      renderTabs();
      tab("health").focus();

      press("End");
      expect(selected()).toEqual(["knowledge"]);
      press("Home");
      expect(selected()).toEqual(["health"]);
    });

    it("keep the page from scrolling on the keys they use, and leave the other keys alone", () => {
      renderTabs();
      tab("health").focus();

      // fireEvent answers false when the event was cancelled
      for (const key of ["ArrowRight", "ArrowLeft", "Home", "End"]) {
        expect(fireEvent.keyDown(document.activeElement ?? document.body, { key }), key).toBe(false);
      }
      fireEvent.click(tab("memory"));
      for (const key of ["Tab", "Enter", "a", "ArrowDown", "constructor"]) {
        expect(fireEvent.keyDown(tab("memory"), { key }), key).toBe(true);
      }
      expect(selected()).toEqual(["memory"]);
    });

    it("open the tabs with the digits, in order", () => {
      renderTabs();

      fireEvent.keyDown(document.body, { key: "2" });
      expect(selected()).toEqual(["memory"]);
      expect(document.activeElement).toBe(tab("memory"));
      fireEvent.keyDown(document.body, { key: "3" });
      expect(selected()).toEqual(["knowledge"]);
      fireEvent.keyDown(document.body, { key: "1" });
      expect(selected()).toEqual(["health"]);
    });

    it("leave alone a digit with no tab, a zero, and a digit with a modifier", () => {
      renderTabs();
      fireEvent.click(tab("memory"));

      fireEvent.keyDown(document.body, { key: "9" });
      fireEvent.keyDown(document.body, { key: "0" });
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
      fireEvent.keyDown(document.body, { key: "1", altKey: true });
      fireEvent.keyDown(document.body, { key: "1", metaKey: true });
      expect(selected()).toEqual(["memory"]);
    });

    it("type a digit into a field instead of changing tabs", () => {
      renderTabs();
      fireEvent.click(tab("memory"));

      fireEvent.keyDown(screen.getByLabelText("field b"), { key: "3" });
      expect(selected()).toEqual(["memory"]);
    });
  });
});
