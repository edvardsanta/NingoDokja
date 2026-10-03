import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";
import { COMMUNICATIONS_TOKEN, HOSTILE_TITLE, Page, hasDisplay, launchApp, sleep, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("built communications tab", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;
  before(async () => {
    orchestrator = await startFakeOrchestrator();
    app = await launchApp(orchestrator.endpoint, COMMUNICATIONS_TOKEN);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("document.querySelectorAll('[role=tab]').length === 6");
  });
  after(async () => { page?.close(); await app?.stop(); await orchestrator?.stop(); });
  it("reads messages through the authenticated shell and renders them as text", async () => {
    await page.evaluate("document.querySelectorAll('[role=tab]')[5].click()");
    await page.waitFor("document.querySelectorAll('.conversation li').length === 2");
    assert.equal(await page.evaluate("document.querySelector('.message-content').textContent"), HOSTILE_TITLE);
    assert.equal(await page.evaluate("document.querySelectorAll('.conversation img, .conversation script').length"), 0);
    const result = await page.evaluate("window.dokja.request('communications.history', { channel_id: '123' })");
    assert.doesNotMatch(JSON.stringify(result), /private-field|test-communications-token/);
    assert.equal(await page.evaluate("window.__owned === undefined"), true);
  });
  it("does not send without real input", async () => {
    const result = await page.evaluate<{ok: boolean}>("window.dokja.request('communications.send', { channel_id: '123', content: 'hello' })");
    assert.equal(result.ok, false);
    assert.equal(orchestrator.seen.filter((item) => item.type === "communications.send").length, 0);
  });
  it("holds a real-input send for native confirmation before touching the orchestrator", async () => {
    await page.click(5, 5);
    await page.evaluate("(window.__sent = false, window.dokja.request('communications.send', { channel_id: '123', content: 'hello' }).then(answer => window.__sent = answer), 'started')");
    await sleep(1800);
    assert.equal(await page.evaluate("window.__sent"), false);
    assert.equal(orchestrator.seen.filter((item) => item.type === "communications.send").length, 0);
  });
});
