import { act, fireEvent, render, screen } from "@testing-library/react";
import { createRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { Drawer } from "../../src/renderer/cards/drawer.js";
import { Button, Notice } from "../../src/renderer/cards/ui.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";

describe("Button", () => {
  it("never submits a form unless it is asked to", () => {
    const submit = vi.fn((event: Event) => event.preventDefault());
    render(<form onSubmit={(event) => submit(event.nativeEvent)}><Button>Plain</Button><Button type="submit">Send</Button></form>);
    fireEvent.click(screen.getByRole("button", { name: "Plain" }));
    expect(submit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(submit).toHaveBeenCalledOnce();
  });

  it("names its look without class strings, and leaves everything else to the element", () => {
    const ref = createRef<HTMLButtonElement>();
    render(<Button ref={ref} variant="quiet" size="compact" extra="conversation-delete" aria-label="Delete it" disabled>Delete</Button>);
    const button = screen.getByRole("button", { name: "Delete it" }) as HTMLButtonElement;
    expect(button.className).toBe("quiet compact conversation-delete");
    expect(button.disabled).toBe(true);
    expect(ref.current).toBe(button);
    render(<Button>Bare</Button>);
    expect(screen.getByRole("button", { name: "Bare" }).hasAttribute("class")).toBe(false);
  });
});

describe("Notice", () => {
  it("announces only what it is told to, politely or at once", () => {
    render(<>
      <Notice announce="polite">Waiting</Notice>
      <Notice announce="assertive">Failed</Notice>
      <Notice>Quiet</Notice>
    </>);
    expect(screen.getByRole("status").textContent).toBe("Waiting");
    expect(screen.getByRole("alert").textContent).toBe("Failed");
    expect(screen.getByText("Quiet").hasAttribute("role")).toBe(false);
  });

  it("draws a boxed note with its tone, and a tone needs the box", () => {
    render(<><Notice boxed tone="problem">Boxed</Notice><Notice tone="problem">Plain</Notice></>);
    const boxed = screen.getByText("Boxed");
    expect(boxed.className).toBe("note");
    expect(boxed.dataset.tone).toBe("problem");
    expect(screen.getByText("Plain").hasAttribute("data-tone")).toBe(false);
  });
});

describe("Drawer", () => {
  function Host() {
    const [open, setOpen] = useState(false);
    return <I18nProvider locale="en">
      <Button onClick={() => setOpen(true)}>Open</Button>
      {open && <Drawer title="Panel" onClose={() => setOpen(false)}><input aria-label="First" /><Button>Last</Button></Drawer>}
    </I18nProvider>;
  }

  it("moves the focus in, keeps Tab inside, closes on Escape and gives the focus back", () => {
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Open" });
    opener.focus();
    fireEvent.click(opener);
    const first = screen.getByRole("textbox", { name: "First" });
    expect(document.activeElement).toBe(first);

    const last = screen.getByRole("button", { name: "Last" });
    last.focus();
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).not.toBe(opener);
    expect(screen.getByRole("dialog", { name: "Panel" }).contains(document.activeElement)).toBe(true);

    act(() => { fireEvent.keyDown(document.activeElement as Element, { key: "Escape" }); });
    expect(screen.queryByRole("textbox", { name: "First" })).toBeNull();
    expect(document.activeElement).toBe(opener);
  });
});
