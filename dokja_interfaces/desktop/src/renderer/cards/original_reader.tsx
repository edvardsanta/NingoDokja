import { useEffect, useRef, useState, type ReactNode } from "react";
import { unzipSync, strFromU8 } from "fflate";
import type { OriginalReading, PassageLocator } from "../../shared/passages.js";
import { passageText } from "../../shared/passages.js";
import { useTranslate } from "../i18n/context.js";
import { Button, Notice } from "./ui.js";

// Strip extraction's Markdown block markers and normalize wrapping/ligatures for display matching.
// The persisted locator itself always addresses the unchanged extracted text.
export function displayText(text: string): string {
  return text.replace(/^\s*(?:#{1,6} |[-*] )/gm, "").normalize("NFKC").replace(/\s/g, "");
}

// Locate the cited occurrence, not simply the first matching sentence in a chapter/page.
export function displaySpan(original: string, start: number, end: number, displayed: string): [number, number] | undefined {
  const [before, selected] = passageText(original, start, end);
  const target = displayText(selected);
  if (!target) return undefined;
  const prefix = displayText(before);
  const all = displayText(original);
  const view = displayText(displayed);
  // Full-unit alignment distinguishes repeated passages and fails closed on extraction differences.
  const unitAt = view.indexOf(all);
  if (unitAt >= 0 && view.indexOf(all, unitAt + 1) < 0) return [unitAt + prefix.length, unitAt + prefix.length + target.length];
  // A unique quoted span can still be highlighted when headers/footers differ between extractors.
  const at = view.indexOf(target);
  if (at >= 0 && view.indexOf(target, at + 1) < 0) return [at, at + target.length];
  return undefined;
}

async function bytesOf(reading: OriginalReading): Promise<Uint8Array<ArrayBuffer>> {
  const bytes = Uint8Array.from(atob(reading.content_b64!), (c) => c.charCodeAt(0));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const version = Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
  if (version !== reading.file_hash) throw new Error("source version mismatch");
  return bytes;
}

function PDFPage({ reading, cited }: { reading: OriginalReading; cited?: PassageLocator }) {
  const t = useTranslate();
  const canvas = useRef<HTMLCanvasElement>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "unresolved" | "error">("loading");
  useEffect(() => {
    let gone = false;
    let destroy: (() => void) | undefined;
    setStatus("loading");
    void (async () => {
      const bytes = await bytesOf(reading);
      const pdfjs = await import("pdfjs-dist");
      const worker = await import("pdfjs-dist/build/pdf.worker.min.mjs?url");
      if (gone) return;
      pdfjs.GlobalWorkerOptions.workerSrc = worker.default;
      const loading = pdfjs.getDocument({ data: bytes, disableFontFace: true, useSystemFonts: true });
      destroy = () => { void loading.destroy(); };
      const pdf = await loading.promise;
      const page = await pdf.getPage(reading.locator!.page!);
      const viewport = page.getViewport({ scale: 1.4 });
      const element = canvas.current;
      if (gone || !element) return;
      const context = element.getContext("2d");
      if (!context) throw new Error("canvas unavailable");
      element.width = Math.ceil(viewport.width);
      element.height = Math.ceil(viewport.height);
      // A single-page snapshot must also finish when Electron is hidden or throttled.
      const render = page.render({ canvas: element, canvasContext: context, viewport, intent: "print", annotationMode: pdfjs.AnnotationMode.DISABLE });
      await render.promise;
      if (gone) return;
      if (!cited || cited.unit !== reading.locator!.unit) { setStatus("ready"); return; }
      const content = await page.getTextContent();
      const items = content.items.filter((item) => "str" in item);
      const displayed = items.map((item) => item.str).join("");
      const span = displaySpan(reading.unit!.text, cited.start, cited.end, displayed);
      if (!span) { setStatus("unresolved"); return; }
      let offset = 0;
      context.fillStyle = "rgba(255, 190, 0, 0.32)";
      for (const item of items) {
        const length = displayText(item.str).length;
        const from = Math.max(span[0], offset), to = Math.min(span[1], offset + length);
        if (to > from && length > 0) {
          const transform = pdfjs.Util.transform(viewport.transform, item.transform);
          const height = Math.hypot(transform[2]!, transform[3]!);
          // Highlight complete intersecting text runs: their glyph geometry belongs to the PDF.
          context.fillRect(transform[4]!, transform[5]! - height, Math.max(item.width * viewport.scale, 1), height * 1.2);
        }
        offset += length;
      }
      setStatus("ready");
    })().catch(() => { if (!gone) setStatus("error"); });
    return () => { gone = true; destroy?.(); };
  }, [reading, cited]);
  return <>
    {status === "loading" && <Notice announce="polite">{t("source_loading")}</Notice>}
    {(status === "error" || status === "unresolved") && <Notice announce="polite">{t(status === "error" ? "source_unavailable" : "source_highlight_unresolved")}</Notice>}
    <canvas ref={canvas} data-state={status} className="original-pdf" role="img" aria-label={t("source_pdf_page", { page: reading.locator!.page!, label: reading.unit!.label })} />
  </>;
}

const OMIT = new Set(["script", "style", "head", "nav", "footer", "aside", "form", "iframe", "object", "embed", "svg", "noscript", "template"]);
const ELEMENTS = new Set(["p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "em", "strong", "i", "b", "sup", "sub", "ul", "ol", "li", "br", "hr", "table", "tbody", "tr", "td", "th", "pre", "span"]);

// Render only semantic text from the original XHTML. No source HTML, styles, attributes or URLs
// enter the app's DOM; font changes preserve the chapter + Unicode range locator.
export function epubContent(markup: string, original: string, cited?: PassageLocator): { content: ReactNode; highlighted: boolean } {
  const parsed = new DOMParser().parseFromString(markup, "text/html");
  parsed.querySelectorAll([...OMIT].join(",")).forEach((node) => node.remove());
  const displayed = parsed.body.textContent ?? "";
  const span = cited ? displaySpan(original, cited.start, cited.end, displayed) : undefined;
  let offset = 0;
  function render(node: Node, key: number): ReactNode {
    if (node.nodeType === Node.TEXT_NODE) {
      const value = node.textContent ?? "";
      const pieces: ReactNode[] = [];
      let buffer = "", marked = false;
      const flush = () => { if (buffer) pieces.push(marked ? <mark key={pieces.length}>{buffer}</mark> : buffer); buffer = ""; };
      for (const point of value) {
        const length = displayText(point).length;
        const selected = Boolean(span && offset < span[1] && offset + length > span[0]);
        if (selected !== marked) { flush(); marked = selected; }
        buffer += point;
        offset += length;
      }
      flush();
      return <span key={key}>{pieces}</span>;
    }
    if (!(node instanceof Element)) return null;
    const children = Array.from(node.childNodes, render);
    const tag = node.tagName.toLowerCase();
    // Source anchors remain text; navigation is provided by the chapter selector.
    if (!ELEMENTS.has(tag)) return <span key={key}>{children}</span>;
    const Tag = tag as "div";
    return <Tag key={key}>{children}</Tag>;
  }
  return { content: Array.from(parsed.body.childNodes, render), highlighted: Boolean(span) };
}

function EPUBChapter({ reading, cited }: { reading: OriginalReading; cited?: PassageLocator }) {
  const t = useTranslate();
  const body = useRef<HTMLDivElement>(null);
  const [rendered, setRendered] = useState<ReturnType<typeof epubContent>>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let gone = false;
    setRendered(undefined); setFailed(false);
    void bytesOf(reading).then((bytes) => {
      if (gone) return;
      const href = reading.unit!.href!;
      const files = unzipSync(bytes, { filter: (file) => file.name === href && file.originalSize <= 40_000_000 });
      if (!files[href]) throw new Error("chapter unavailable");
      setRendered(epubContent(strFromU8(files[href]), reading.unit!.text, cited?.unit === reading.locator!.unit ? cited : undefined));
    }).catch(() => { if (!gone) setFailed(true); });
    return () => { gone = true; };
  }, [reading, cited]);
  useEffect(() => { body.current?.querySelector("mark")?.scrollIntoView?.({ block: "center" }); }, [rendered]);
  return <>
    {!rendered && !failed && <Notice announce="polite">{t("source_loading")}</Notice>}
    {failed && <Notice announce="polite">{t("source_unavailable")}</Notice>}
    {rendered && cited?.unit === reading.locator!.unit && !rendered.highlighted && <Notice announce="polite">{t("source_highlight_unresolved")}</Notice>}
    <div ref={body} className="original-epub">{rendered?.content}</div>
  </>;
}

export function OriginalReader({ reading, cited, onNavigate }: { reading: OriginalReading; cited: PassageLocator; onNavigate: (locator: PassageLocator) => void }) {
  const t = useTranslate();
  const text = useRef<HTMLPreElement>(null);
  useEffect(() => { text.current?.querySelector("mark")?.scrollIntoView?.({ block: "center" }); }, [reading]);
  if (reading.state !== "resolved" || !reading.unit || !reading.locator) return <Notice announce="polite">{t("source_unavailable")}</Notice>;
  const loc = reading.locator;
  const [before, selected, after] = passageText(reading.unit.text, cited.unit === loc.unit ? cited.start : 0, cited.unit === loc.unit ? cited.end : 0);
  return <section className="original-reader" aria-label={t("source_reader")}>
    <header>
      <p>{loc.format === "pdf" ? t("source_pdf_page", { page: loc.page!, label: reading.unit.label }) : `${reading.unit.label} · ${reading.unit.href}`}</p>
      <p className="detail">{t("source_version", { version: loc.version.slice(0, 12) })}</p>
      <Button size="compact" onClick={() => onNavigate(cited)}>{t("source_return")}</Button>
    </header>
    {reading.navigation && <label>{t(loc.format === "pdf" ? "source_page" : "source_chapter")}
      <select value={loc.unit} onChange={(event) => {
        const unit = Number(event.target.value), entry = reading.navigation![unit]!;
        onNavigate({ version: loc.version, format: loc.format, unit, start: 0, end: entry.chars ?? 1, page: entry.page, href: entry.href, label: entry.label });
      }}>{reading.navigation.map((unit, i) => <option key={i} value={i} disabled={!unit.chars}>{unit.page ? `${unit.page} · ${unit.label}` : unit.label}</option>)}</select>
    </label>}
    {loc.format === "pdf" ? <PDFPage reading={reading} cited={cited} /> : <EPUBChapter reading={reading} cited={cited} />}
    <details open className="original-context"><summary>{t("source_context")}</summary>
      <pre ref={text}>{before}<mark>{selected}</mark>{after}</pre>
    </details>
  </section>;
}
