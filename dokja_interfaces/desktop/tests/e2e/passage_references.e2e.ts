import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { after, before, describe, it } from "node:test";
import { zipSync, strToU8 } from "fflate";
import { Page, hasDisplay, launchApp, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

const quote = "Marriage decisions depend on material resources.";
function pdfFixture(): Buffer {
  const content = `BT /F1 12 Tf 14 TL 20 150 Td (${quote}) Tj ET`;
  const objects = ["<</Type/Catalog/Pages 2 0 R>>", "<</Type/Pages/Kids[3 0 R]/Count 1>>", "<</Type/Page/Parent 2 0 R/MediaBox[0 0 400 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>", `<</Length ${content.length}>>\nstream\n${content}\nendstream`, "<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>"];
  let data = "%PDF-1.4\n";
  const offsets = objects.map((object, i) => { const offset = data.length; data += `${i + 1} 0 obj\n${object}\nendobj\n`; return offset; });
  const xref = data.length;
  data += `xref\n0 6\n0000000000 65535 f \n${offsets.map((offset) => `${String(offset).padStart(10, "0")} 00000 n \n`).join("")}trailer\n<</Root 1 0 R/Size 6>>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(data);
}
const chapter = "# Marriage\n\n" + quote;
const markup = `<html><body><h1>Marriage</h1><p>${quote}</p><script>window.__sourceExecuted=true</script></body></html>`;
const epub = Buffer.from(zipSync({
  mimetype: strToU8("application/epub+zip"),
  "META-INF/container.xml": strToU8('<container><rootfiles><rootfile full-path="package.opf"/></rootfiles></container>'),
  "package.opf": strToU8('<package><manifest><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/></spine></package>'),
  "chapter.xhtml": strToU8(markup),
}));
const id = "1".padStart(32, "0");
const conversation = { id, kind: "general", title: "Source discussion", turns: 1, revision: 1 };
const records = [pdfFixture(), epub].map((bytes, index) => {
  const format = index === 0 ? "pdf" : "epub";
  const text = index === 0 ? quote : chapter;
  const start = index === 0 ? 0 : chapter.indexOf(quote);
  const locator = { version: createHash("sha256").update(bytes).digest("hex"), format, unit: 0, start, end: text.length, passage_id: "",
    ...(format === "pdf" ? { page: 1, label: "7" } : { href: "chapter.xhtml", label: "Marriage" }) };
  return { bytes, text, locator, source_id: `file:study.${format}`, number: index + 1, title: `Study ${format.toUpperCase()}`, excerpt: quote };
});
const wrap = (domain: string, result: unknown) => ({ status: "ok", result: { domain, result } });

describe("exact PDF and EPUB passages in archived replies", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator, app: RunningApp, page: Page;
  before(async () => {
    orchestrator = await startFakeOrchestrator((type, payload) => {
      if (type === "conversation.list") return wrap("conversation", { conversations: [conversation] });
      if (type === "conversation.read") return wrap("conversation", { conversation, turns: [{
        view: "1", id: "2".padStart(32, "0"), seq: 1, send_id: "send-0001", content: "Interpret the book", state: "completed",
        reply: "My interpretation rests on these passages [1, 2].\n\n📚 [1] Study PDF · [2] Study EPUB",
        sources: records.map(({ bytes, text, ...source }) => source),
      }] });
      if (type === "knowledge.list" && payload.locator) {
        const source = records.find((entry) => entry.source_id === payload.source_id)!;
        return wrap("knowledge", { total: 1, offset: 0, documents: [{ source_id: source.source_id, title: source.title, kind: "book", chunks: 1, embedded: 1,
          reading: { state: "resolved", version: source.locator.version, file_hash: source.locator.version, format: source.locator.format, filename: `study.${source.locator.format}`,
            content_b64: source.bytes.toString("base64"), locator: source.locator, unit: { text: source.text, ...source.locator },
            navigation: [{ chars: source.text.length, ...source.locator }],
          },
        }] });
      }
      return undefined;
    });
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("document.querySelectorAll('button.cite').length === 2");
  });
  after(async () => { page?.close(); await app?.stop(); await orchestrator?.stop(); });

  it("opens the referenced PDF page and highlights it in the original geometry", async () => {
    await page.evaluate("document.querySelectorAll('button.cite')[0].click()");
    await page.waitFor("['ready','error','unresolved'].includes(document.querySelector('.original-pdf')?.dataset.state)").catch(async () => { throw new Error(await page.evaluate<string>("JSON.stringify({hidden:document.hidden,canvas:document.querySelector('.original-pdf')?.dataset, text:document.querySelector('.drawer-body')?.textContent})")); });
    assert.equal(await page.evaluate("document.querySelector('.original-pdf')?.dataset.state"), "ready");
    assert.equal(await page.evaluate("document.querySelector('.original-context mark')?.textContent"), quote);
    assert.equal(await page.evaluate("document.querySelector('.original-reader header')?.textContent?.includes('printed label 7')"), true);
    assert.deepEqual(orchestrator.seen.filter((event) => event.type === "knowledge.list").at(-1)?.payload?.locator, records[0]!.locator);
    assert.equal(await page.evaluate("(() => {const c=document.querySelector('.original-pdf');const d=c.getContext('2d').getImageData(0,0,c.width,c.height).data;for(let i=0;i<d.length;i+=4){if(d[i]>240 && d[i+1]>190 && d[i+2]<220)return true;}return false;})()"), true);
  });

  it("opens the original EPUB chapter at its exact range without executing source scripts", async () => {
    await page.evaluate("document.querySelector('[role=dialog] .drawer-close').click()");
    await page.evaluate("document.querySelectorAll('button.cite')[1].click()");
    await page.waitFor("document.querySelector('.original-epub mark')?.textContent");
    assert.equal(await page.evaluate("document.querySelector('.original-epub mark')?.textContent"), quote);
    assert.equal(await page.evaluate("window.__sourceExecuted === undefined"), true);
    assert.deepEqual(orchestrator.seen.filter((event) => event.type === "knowledge.list").at(-1)?.payload?.locator, records[1]!.locator);
  });
});
