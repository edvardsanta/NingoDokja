import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { epubContent, displaySpan } from "../../src/renderer/cards/original_reader.js";
import { ReplyBody } from "../../src/renderer/cards/reply_sources.js";
import { DocumentView } from "../../src/renderer/cards/document_view.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { passageLocator, passageText } from "../../src/shared/passages.js";
import { ok, routedTransport } from "./support.js";

const locator = { version: "a".repeat(64), format: "epub" as const, unit: 1, start: 0, end: 4, href: "chapter.xhtml", label: "Chapter two", passage_id: "b".repeat(64) };

describe("exact passage references", () => {
  it("opens each reference in a group independently, retaining distinct anchors from one book", () => {
    const opened: unknown[] = [];
    const sources = [{ number: 1, id: "book", title: "Book", excerpt: "One", locator: { ...locator, unit: 0 } }, { number: 2, id: "book", title: "Book", excerpt: "Two", locator }];
    render(<I18nProvider locale="en"><ReplyBody reply="An interpretation [1, 2]." kept={sources} onOpen={(source) => opened.push(source)} /></I18nProvider>);
    const buttons = document.querySelectorAll("button.cite");
    expect(buttons.length).toBe(2);
    fireEvent.click(buttons[0]!); fireEvent.click(buttons[1]!);
    expect(opened).toEqual(sources);
    fireEvent.focus(buttons[1]!);
    expect(screen.getByRole("tooltip").textContent).toContain("Chapter two");
    expect(screen.getByRole("tooltip").textContent).toContain("Two");
  });
  it("counts Unicode code points and distinguishes repeated occurrences", () => {
    expect(passageText("🙂 alpha 🙂 beta", 8, 14)).toEqual(["🙂 alpha ", "🙂 beta", ""]);
    const chapter = "# Chapter\n\nRepeated sentence.\n\nRepeated sentence.";
    const start = chapter.lastIndexOf("Repeated");
    const shown = "ChapterRepeated sentence.Repeated sentence.";
    expect(displaySpan(chapter, start, chapter.length, shown)).toEqual([24, 41]);
    expect(displaySpan("Repeated sentence. Different context.", 0, 18, "Repeated sentence.Repeated sentence.")).toBeUndefined();
  });
  it("renders original chapter text with highlights while discarding active source content", () => {
    const original = "# Chapter\n\nMarriage evidence.";
    const cited = { ...locator, start: 11, end: original.length };
    const chapter = epubContent('<html><head><style>body{display:none}</style></head><body><h1>Chapter</h1><p onclick="alert(1)">Marriage evidence.</p><script>alert(1)</script><img src="https://example.com/tracker"><a href="javascript:alert(1)">note</a></body></html>', original, cited);
    const view = render(<>{chapter.content}</>);
    expect(chapter.highlighted).toBe(true);
    expect(view.container.querySelector("mark")?.textContent).toBe("Marriage evidence.");
    expect(view.container.querySelector("script, style, img, a, [onclick]")).toBeNull();
  });
  it("sends the archived locator when opening a source and reports a missing original explicitly", async () => {
    const { transport, calls } = routedTransport({ "knowledge.list": ok({ total: 1, offset: 0, documents: [{ id: "book", title: "Book", kind: "book", reference: "", tags: [], chunks: 1, embedded: 1, updatedAt: "", reading: { state: "unavailable" } }] }) });
    render(<I18nProvider locale="en"><DocumentView transport={transport} id="book" locator={locator} /></I18nProvider>);
    await screen.findByText(/original version or its exact passage is unavailable/);
    expect(calls[0]?.payload).toEqual({ source_id: "book", include_text: true, locator });
    expect(document.querySelector(".document-text")).toBeNull();
  });
  it("rejects malformed or unsafe locators before opening a file", () => {
    expect(passageLocator(locator)).toEqual(locator);
    for (const invalid of [{ ...locator, href: "../outside.xhtml" }, { ...locator, start: -1 }, { ...locator, version: "stale" }, { ...locator, end: 0 }]) expect(passageLocator(invalid)).toBeUndefined();
  });
});
