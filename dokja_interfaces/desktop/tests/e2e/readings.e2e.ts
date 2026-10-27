import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";
import { Page, hasDisplay, launchApp, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

const FULL_TEXT = "Entrance antiphon\n\nA complete passage beyond the summary.\n\n<img src=x onerror=window.__readingOwned=true>";

describe("reading previews and filters in the built app", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;
  before(async () => {
    orchestrator = await startFakeOrchestrator((type, payload) => type === "digest.items" ? {
      status: "ok", result: { domain: "digest", result: {
        items: [{ id: "daily:2026-10-27", plugin: "source-1", title: "Daily reading", summary: "Short excerpt", body: FULL_TEXT, source: "First source", published: "2026-10-27T03:00:00Z" }],
        total: 1, more: 0, offset: payload.offset ?? 0, updated: "2026-10-27T03:00:00Z",
      } },
    } : undefined);
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
  });
  after(async () => { page?.close(); await app?.stop(); await orchestrator?.stop(); });

  it("opens full text and sends the selected source and dates through IPC", async () => {
    await page.waitFor("[...document.querySelectorAll('[role=tab]')].some(tab=>tab.textContent==='Library')");
    await page.evaluate("[...document.querySelectorAll('[role=tab]')].find(tab=>tab.textContent==='Library').click()");
    await page.waitFor("[...document.querySelectorAll('[role=tab]')].some(tab=>tab.textContent==='Readings')");
    await page.evaluate("[...document.querySelectorAll('[role=tab]')].find(tab=>tab.textContent==='Readings').click()");
    await page.waitFor("document.querySelector('.reading-preview summary') !== null");
    await page.evaluate("document.querySelector('.reading-preview summary').click()");
    assert.equal(await page.waitFor("document.querySelector('.reading-preview-text')?.textContent"), FULL_TEXT);
    assert.equal(await page.evaluate("window.__readingOwned === undefined && document.querySelector('.reading-preview img') === null"), true);
    await page.evaluate(`(() => {
      const form = document.querySelector('.reading-filters');
      form.querySelector('select').value = 'source-1';
      form.querySelector('select').dispatchEvent(new Event('change', { bubbles: true }));
      const set = (node, value) => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(node,value); node.dispatchEvent(new Event('input', { bubbles:true })); };
      set(form.querySelector('input[type=search]'), 'passage');
      set(form.querySelectorAll('input[type=date]')[0], '2026-10-27');
      set(form.querySelectorAll('input[type=date]')[1], '2026-10-27');
    })()`);
    await page.evaluate("document.querySelector('.reading-filters button[type=submit]').click()");
    await page.waitFor("document.querySelector('[role=status]')?.textContent?.includes('1 matching readings')");
    const request = orchestrator.seen.filter(event => event.type === "digest.items").at(-1);
    assert.deepEqual(request?.payload, { limit: 8, offset: 0, source: "source-1", query: "passage", from: "2026-10-27", to: "2026-10-27" });
    assert.equal(await page.evaluate("document.querySelector('.reading-filters').scrollWidth <= document.querySelector('.reading-filters').clientWidth"), true);
  });
});
