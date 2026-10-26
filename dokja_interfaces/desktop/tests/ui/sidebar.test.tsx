import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import type { CardEntry } from "../../src/renderer/cards/registry.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { Sidebar } from "../../src/renderer/sidebar.js";
import { StatusProvider } from "../../src/renderer/status_context.js";
import type { StatusReply } from "../../src/shared/replies.js";
import type { Transport } from "../../src/shared/transport.js";
import { routedTransport, statusReply } from "./support.js";

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
    { kind: "a", labelId: "nav_assistant", Card: card("a") },
    { kind: "b", labelId: "nav_notebook", Card: card("b") },
    { kind: "c", labelId: "nav_library", Card: card("c") },
  ];
}

function renderTabs(mounted: string[] = [], start?: string) {
  render(
    <I18nProvider locale="en">
      <Sidebar entries={entries(mounted)} transport={NO_TRANSPORT} start={start} />
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
    expect(screen.getAllByRole("tab").map((each) => each.textContent)).toEqual(["Talk", "Notebook", "Library"]);
    expect(selected()).toEqual(["Talk"]);
    screen.getByText("card a");
    expect(screen.queryByText("card b")).toBeNull();
    expect(new Set(mounted)).toEqual(new Set(["a"]));
  });

  it("mounts a card the first time its workspace is chosen", () => {
    const mounted = renderTabs();
    fireEvent.click(tab("Notebook"));

    expect(selected()).toEqual(["Notebook"]);
    screen.getByText("card b");
    expect(new Set(mounted)).toEqual(new Set(["a", "b"]));
  });

  it("keeps a card, with what it showed, while another workspace is open", () => {
    renderTabs();
    fireEvent.click(screen.getByRole("button", { name: "count a: 0" }));
    fireEvent.click(tab("Notebook"));
    fireEvent.click(tab("Talk"));

    screen.getByRole("button", { name: "count a: 1" });
    // every tab has its panel; the open one is the only one exposed, and the card of a tab that was
    // opened stays inside its hidden panel
    expect(screen.getAllByRole("tabpanel", { hidden: true })).toHaveLength(3);
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    expect(document.body.textContent).toContain("card b");
    fireEvent.click(tab("Notebook"));
    fireEvent.click(screen.getByRole("button", { name: "count b: 0" }));
    fireEvent.click(tab("Talk"));
    fireEvent.click(tab("Notebook"));
    screen.getByRole("button", { name: "count b: 1" });
  });

  it("hides the card of a workspace that is not open from the page and from screen readers", () => {
    renderTabs();
    fireEvent.click(tab("Notebook"));

    const [first, second] = screen.getAllByRole("tabpanel", { hidden: true });
    expect(first?.hasAttribute("hidden")).toBe(true);
    expect(second?.hasAttribute("hidden")).toBe(false);
  });

  it("ties each entry to its panel", () => {
    renderTabs();
    fireEvent.click(tab("Notebook"));

    const panel = screen.getByRole("tabpanel");
    expect(tab("Notebook").getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.getAttribute("aria-labelledby")).toBe(tab("Notebook").id);
  });

  it("lets only the open entry take the Tab key", () => {
    renderTabs();

    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([0, -1, -1]);
    fireEvent.click(tab("Library"));
    expect(screen.getAllByRole("tab").map((each) => each.tabIndex)).toEqual([-1, -1, 0]);
  });

  describe("with the keyboard", () => {
    const press = (key: string) => fireEvent.keyDown(document.activeElement ?? document.body, { key });

    it("moves with the up and down arrows, wrapping at both ends, and focuses the entry they open", () => {
      renderTabs();
      tab("Talk").focus();

      press("ArrowDown");
      expect(selected()).toEqual(["Notebook"]);
      expect(document.activeElement).toBe(tab("Notebook"));
      press("ArrowDown");
      press("ArrowDown");
      expect(selected()).toEqual(["Talk"]);
      press("ArrowUp");
      expect(selected()).toEqual(["Library"]);
      expect(document.activeElement).toBe(tab("Library"));
    });

    it("jumps to the first and last entry with Home and End", () => {
      renderTabs();
      tab("Talk").focus();

      press("End");
      expect(selected()).toEqual(["Library"]);
      press("Home");
      expect(selected()).toEqual(["Talk"]);
    });

    it("keeps the page from scrolling on the keys it uses, and leaves the other keys alone", () => {
      renderTabs();
      tab("Talk").focus();

      // fireEvent answers false when the event was cancelled
      for (const key of ["ArrowDown", "ArrowUp", "Home", "End"]) {
        expect(fireEvent.keyDown(document.activeElement ?? document.body, { key }), key).toBe(false);
      }
      fireEvent.click(tab("Notebook"));
      for (const key of ["Tab", "Enter", "a", "ArrowRight", "constructor"]) {
        expect(fireEvent.keyDown(tab("Notebook"), { key }), key).toBe(true);
      }
      expect(selected()).toEqual(["Notebook"]);
    });

    it("opens the workspaces with Ctrl and a digit, in order", () => {
      renderTabs();

      fireEvent.keyDown(document.body, { key: "2", ctrlKey: true });
      expect(selected()).toEqual(["Notebook"]);
      expect(document.activeElement).toBe(tab("Notebook"));
      fireEvent.keyDown(document.body, { key: "3", ctrlKey: true });
      expect(selected()).toEqual(["Library"]);
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
      expect(selected()).toEqual(["Talk"]);
    });

    it("leaves alone a bare digit, a digit with no workspace, a zero, and other modifiers", () => {
      renderTabs();
      fireEvent.click(tab("Notebook"));

      fireEvent.keyDown(document.body, { key: "1" });
      fireEvent.keyDown(document.body, { key: "9", ctrlKey: true });
      fireEvent.keyDown(document.body, { key: "0", ctrlKey: true });
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true, altKey: true });
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true, shiftKey: true });
      fireEvent.keyDown(document.body, { key: "1", metaKey: true });
      expect(selected()).toEqual(["Notebook"]);
    });

    it("leaves the workspaces alone while a dialog is open over them", () => {
      renderTabs();
      fireEvent.click(tab("Notebook"));
      const dialog = document.createElement("div");
      dialog.setAttribute("role", "dialog");
      dialog.setAttribute("aria-modal", "true");
      document.body.append(dialog);
      try {
        fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
        expect(selected()).toEqual(["Notebook"]);
      } finally {
        dialog.remove();
      }
      fireEvent.keyDown(document.body, { key: "1", ctrlKey: true });
      expect(selected()).toEqual(["Talk"]);
    });

    it("types a bare digit into a field, and still opens a workspace on Ctrl and a digit from it", () => {
      renderTabs();
      fireEvent.click(tab("Notebook"));

      fireEvent.keyDown(screen.getByLabelText("field b"), { key: "3" });
      expect(selected()).toEqual(["Notebook"]);
      expect(fireEvent.keyDown(screen.getByLabelText("field b"), { key: "3", ctrlKey: true })).toBe(false);
      expect(selected()).toEqual(["Library"]);
    });
  });

  describe("Ctrl+K", () => {
    // The conversation is "a", with a field that declares the shortcut, as Talk's composer does.
    const composing: CardEntry[] = [
      { kind: "a", labelId: "nav_assistant", Card: () => <textarea aria-label="message" aria-keyshortcuts="Control+K" /> },
      { kind: "b", labelId: "nav_notebook", Card: () => <input aria-label="field b" /> },
    ];
    const renderComposing = (start?: string) =>
      render(
        <I18nProvider locale="en">
          <Sidebar entries={composing} transport={NO_TRANSPORT} start={start} composer="a" />
        </I18nProvider>,
      );

    it("opens the conversation from another workspace, a field included, and puts the focus in its message", () => {
      renderComposing("b");
      screen.getByLabelText("field b").focus();

      expect(fireEvent.keyDown(screen.getByLabelText("field b"), { key: "k", ctrlKey: true })).toBe(false);
      expect(selected()).toEqual(["Talk"]);
      expect(document.activeElement).toBe(screen.getByLabelText("message"));
    });

    it("puts the focus back in the message when the conversation is already open", () => {
      renderComposing();
      tab("Talk").focus();

      fireEvent.keyDown(document.body, { key: "K", ctrlKey: true });
      expect(document.activeElement).toBe(screen.getByLabelText("message"));
    });

    it("leaves a bare k, other modifiers and an open dialog alone", () => {
      renderComposing("b");
      fireEvent.keyDown(document.body, { key: "k" });
      fireEvent.keyDown(document.body, { key: "k", ctrlKey: true, shiftKey: true });
      fireEvent.keyDown(document.body, { key: "k", ctrlKey: true, altKey: true });
      const dialog = document.createElement("div");
      dialog.setAttribute("aria-modal", "true");
      document.body.append(dialog);
      try {
        fireEvent.keyDown(document.body, { key: "k", ctrlKey: true });
      } finally {
        dialog.remove();
      }
      expect(selected()).toEqual(["Notebook"]);
    });

    it("does nothing without a conversation to go to", () => {
      renderTabs([], "b");
      expect(fireEvent.keyDown(document.body, { key: "k", ctrlKey: true })).toBe(true);
      expect(selected()).toEqual(["Notebook"]);
    });
  });

  it("opens on the workspace it is told to start on, and mounts only that one", () => {
    const mounted = renderTabs([], "c");

    expect(selected()).toEqual(["Library"]);
    expect(new Set(mounted)).toEqual(new Set(["c"]));
  });

  describe("the state of each workspace", () => {
    const stand = () => <p>stand-in</p>;
    const withServices: CardEntry[] = [
      { kind: "a", labelId: "nav_library", Card: stand, services: ["knowledge"] },
      { kind: "b", labelId: "nav_messages", Card: stand, services: ["discord", "whatsapp"] },
      { kind: "c", labelId: "nav_assistant", Card: stand },
      { kind: "d", labelId: "nav_system", Card: stand, foot: true },
    ];

    // The tones come from the one status the screen reads, as they do on the real screen.
    const renderWith = (services?: StatusReply["services"]) => {
      const { transport } = routedTransport(services ? { "ningo.status": statusReply(services) } : {});
      return render(
        <I18nProvider locale="en">
          <StatusProvider transport={transport}>
            <Sidebar entries={withServices} transport={NO_TRANSPORT} />
          </StatusProvider>
        </I18nProvider>,
      );
    };

    const dotOf = (name: string) => tab(name).querySelector(".tab-dot")?.getAttribute("data-tone");

    it("shows the worst tone among the services behind each workspace, and no dot for one it does not know", async () => {
      renderWith({
        knowledge: { status: "ok", detail: "" },
        discord: { status: "ok", detail: "", kind: "interface" },
        whatsapp: { status: "error", detail: "", kind: "interface" },
        meme: { status: "disabled", detail: "", enabled: false },
      });

      await waitFor(() => expect(dotOf("Library")).toBe("ok"));
      expect(dotOf("Messages")).toBe("problem");
      expect(dotOf("Talk")).toBeUndefined();
      expect(dotOf("System")).toBeUndefined();
    });

    it("shows no dot before any status arrives", () => {
      renderWith({ knowledge: { status: "ok", detail: "" } });

      expect(document.querySelectorAll(".tab-dot")).toHaveLength(0);
      expect(document.querySelectorAll(".tab-count")).toHaveLength(0);
    });

    it("counts the services with a problem at the foot, and keeps the count out of the entry's name", async () => {
      renderWith({
        knowledge: { status: "error", detail: "" },
        whatsapp: { status: "error", detail: "", kind: "interface" },
        meme: { status: "unchecked", detail: "" },
      });

      await waitFor(() => expect(tab("System").querySelector(".tab-count")?.textContent).toBe("2"));
      expect(tab("System").hasAttribute("data-foot")).toBe(true);
    });

    it("says the state in a word only when something is not well, and describes it to a screen reader", async () => {
      renderWith({
        knowledge: { status: "ok", detail: "" },
        discord: { status: "unchecked", detail: "", kind: "interface" },
        whatsapp: { status: "error", detail: "", kind: "interface" },
      });

      await waitFor(() => expect(tab("Messages").getAttribute("aria-description")).toBe("needs attention"));
      expect(tab("Messages").querySelector(".tab-word")?.textContent).toBe("needs attention");
      expect(tab("Library").hasAttribute("aria-description")).toBe(false);
      expect(tab("Library").querySelector(".tab-word")).toBeNull();
      expect(tab("System").getAttribute("aria-description")).toBe("need attention");
    });

    it("says not checked when the worst behind a workspace has not been checked", async () => {
      renderWith({ knowledge: { status: "unchecked", detail: "" } });

      await waitFor(() => expect(tab("Library").getAttribute("aria-description")).toBe("not checked"));
      expect(tab("System").hasAttribute("aria-description")).toBe(false);
    });

    it("says offline at the foot, and shows no dot, when nothing answers", async () => {
      renderWith();

      await waitFor(() => expect(tab("System").querySelector(".tab-count")?.textContent).toBe("offline"));
      expect(document.querySelectorAll(".tab-dot")).toHaveLength(0);
    });
  });
});
