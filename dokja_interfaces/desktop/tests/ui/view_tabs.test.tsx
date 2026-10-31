import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
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

// A workspace whose first view opens items: they join the strip after the views and close on their own.
function WithItems({ initial }: { initial: string[] }) {
  const [open, setOpen] = useState(initial);
  const { tabs, panels, showItem } = useViewTabs(
    "work",
    [
      { id: "list", label: "List" },
      { id: "other", label: "Other" },
    ],
    open.map((id) => ({ id, label: `Item ${id}`, closeLabel: `Close item ${id}`, onClose: () => setOpen((now) => now.filter((each) => each !== id)) })),
  );
  return (
    <>
      {tabs}
      <button type="button" onClick={() => showItem("a")}>go to a</button>
      {panels(
        (id) => <p>view {id}</p>,
        (id) => <p>body of {id}</p>,
      )}
    </>
  );
}

const names = () => screen.getAllByRole("tab", { hidden: true }).map((tab) => tab.textContent);
const selected = () => screen.getAllByRole("tab").filter((tab) => tab.getAttribute("aria-selected") === "true").map((tab) => tab.textContent);

describe("items opened from a view", () => {
  it("join the strip after the views, with the whole title in a tooltip and a close button each", () => {
    render(<WithItems initial={["a", "b"]} />);

    expect(names()).toEqual(["List", "Other", "Item a", "Item b"]);
    expect(screen.getByRole("tab", { name: "Item a" }).getAttribute("title")).toBe("Item a");
    screen.getByRole("button", { name: "Close item a" });
    // an item has no digit: Alt+3 opens nothing
    expect(screen.getByRole("tab", { name: "Item a" }).hasAttribute("aria-keyshortcuts")).toBe(false);
    expect(fireEvent.keyDown(document.body, { key: "3", altKey: true })).toBe(true);
    expect(selected()).toEqual(["List"]);
  });

  it("are reached with the arrows, one tab stop for the whole strip, wrapping at the ends", () => {
    render(<WithItems initial={["a", "b"]} />);
    const strip = screen.getByRole("tablist");
    expect(screen.getAllByRole("tab").filter((tab) => tab.tabIndex === 0)).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "go to a" }));
    expect(selected()).toEqual(["Item a"]);
    expect(screen.getAllByRole("tab").filter((tab) => tab.tabIndex === 0).map((tab) => tab.textContent)).toEqual(["Item a"]);

    fireEvent.keyDown(screen.getByRole("tab", { name: "Item a" }), { key: "ArrowRight" });
    expect(selected()).toEqual(["Item b"]);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Item b" }));
    fireEvent.keyDown(strip, { key: "ArrowRight" });
    expect(selected()).toEqual(["List"]);
    fireEvent.keyDown(strip, { key: "ArrowLeft" });
    expect(selected()).toEqual(["Item b"]);
  });

  it("show only the body of the one on screen, and a view keeps what it held", () => {
    render(<WithItems initial={["a", "b"]} />);

    fireEvent.click(screen.getByRole("tab", { name: "Item b" }));
    expect(screen.getByText("body of b").closest("[hidden]")).toBeNull();
    expect(screen.getByText("body of a").closest("[hidden]")).not.toBeNull();
    expect(screen.getByText("view list").closest("[hidden]")).not.toBeNull();
  });

  it("close with their button; the one on screen gives way to the first view and the focus follows", () => {
    render(<WithItems initial={["a", "b"]} />);
    fireEvent.click(screen.getByRole("tab", { name: "Item b" }));

    fireEvent.click(screen.getByRole("button", { name: "Close item b" }));
    expect(names()).toEqual(["List", "Other", "Item a"]);
    expect(selected()).toEqual(["List"]);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "List" }));
    expect(screen.queryByText("body of b")).toBeNull();

    // closing one that is not on screen leaves the view where it is
    fireEvent.click(screen.getByRole("button", { name: "Close item a" }));
    expect(names()).toEqual(["List", "Other"]);
    expect(selected()).toEqual(["List"]);
  });

  it("keep only the close button of the item on screen in the tab order", () => {
    render(<WithItems initial={["a", "b"]} />);
    fireEvent.click(screen.getByRole("tab", { name: "Item a" }));

    expect(screen.getByRole("button", { name: "Close item a" }).tabIndex).toBe(0);
    expect(screen.getByRole("button", { name: "Close item b" }).tabIndex).toBe(-1);
  });
});
