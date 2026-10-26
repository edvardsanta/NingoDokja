import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useViewTabs } from "../../src/renderer/cards/view_tabs.js";

// A workspace with three views, as Library and System have. Two of them sit side by side, one
// hidden, as the sidebar keeps a workspace in the background.
function Views({ name }: { name: string }) {
  const { tabs, panels } = useViewTabs(name, [
    { id: "one", label: `${name} one` },
    { id: "two", label: `${name} two` },
    { id: "three", label: `${name} three` },
  ]);
  return <>{tabs}{panels((id) => <p>{name} shows {id}</p>)}</>;
}

function renderBoth() {
  render(
    <>
      <div><Views name="front" /></div>
      <div hidden><Views name="back" /></div>
    </>,
  );
}

const current = (name: string) =>
  screen
    .getAllByRole("tab", { hidden: true })
    .filter((tab) => tab.textContent?.startsWith(name) && tab.getAttribute("aria-selected") === "true")
    .map((tab) => tab.textContent);

describe("the views of a workspace", () => {
  it("opens with Alt and a digit, in order, only in the workspace on screen", () => {
    renderBoth();

    expect(fireEvent.keyDown(document.body, { key: "3", altKey: true })).toBe(false);
    expect(current("front")).toEqual(["front three"]);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "front three" }));
    screen.getByText("front shows three");
    expect(current("back")).toEqual(["back one"]);

    fireEvent.keyDown(document.body, { key: "1", altKey: true });
    expect(current("front")).toEqual(["front one"]);
  });

  it("names each view's shortcut", () => {
    renderBoth();
    expect(screen.getByRole("tab", { name: "front two" }).getAttribute("aria-keyshortcuts")).toBe("Alt+2");
  });

  it("leaves alone a digit with no view, other modifiers and an open dialog", () => {
    renderBoth();

    expect(fireEvent.keyDown(document.body, { key: "4", altKey: true })).toBe(true);
    fireEvent.keyDown(document.body, { key: "2" });
    fireEvent.keyDown(document.body, { key: "2", altKey: true, ctrlKey: true });
    fireEvent.keyDown(document.body, { key: "2", altKey: true, shiftKey: true });
    const dialog = document.createElement("div");
    dialog.setAttribute("aria-modal", "true");
    document.body.append(dialog);
    try {
      fireEvent.keyDown(document.body, { key: "2", altKey: true });
    } finally {
      dialog.remove();
    }
    expect(current("front")).toEqual(["front one"]);
  });
});
