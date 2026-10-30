import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";
import { Page, hasDisplay, launchApp, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("post video fullscreen", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;
  before(async () => {
    orchestrator = await startFakeOrchestrator();
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("document.querySelector('.composer') !== null");
    await page.evaluate(`(() => {
      const fixture = document.createElement('section');
      fixture.id = 'video-fixture';
      fixture.style.cssText = 'position:fixed;inset:20px;z-index:9999;background:var(--surface-page)';
      fixture.innerHTML = '<form class="add-form"><div class="add-post-media"><video id="post-video-test" width="180" height="320" controls aria-label="Test video"></video></div></form><button id="video-fullscreen-test" type="button">Fullscreen</button>';
      document.body.append(fixture);
      document.getElementById('video-fullscreen-test').addEventListener('click', () => {
        document.getElementById('post-video-test').requestFullscreen().catch(error => { window.__fullscreenFailure = error.message; });
      });
    })()`);
  });
  after(async () => { page?.close(); await app?.stop(); await orchestrator?.stop(); });

  it("enters fullscreen on real input and fills the viewport without the preview cap", async () => {
    const position = await page.evaluate<{ x: number; y: number }>(`(() => {
      const box = document.getElementById('video-fullscreen-test').getBoundingClientRect();
      return {x:box.x + box.width / 2, y:box.y + box.height / 2};
    })()`);
    await page.click(position.x, position.y);
    await page.waitFor("document.fullscreenElement?.id === 'post-video-test'");
    const size = await page.evaluate<{ width: number; height: number; vw: number; vh: number; max: string }>(`(() => {
      const video = document.getElementById('post-video-test'); const box = video.getBoundingClientRect();
      return {width:box.width,height:box.height,vw:innerWidth,vh:innerHeight,max:getComputedStyle(video).maxHeight};
    })()`);
    assert.equal(size.max, "none");
    assert.ok(Math.abs(size.width - size.vw) <= 1, JSON.stringify(size));
    assert.ok(Math.abs(size.height - size.vh) <= 1, JSON.stringify(size));
    await page.evaluate("document.exitFullscreen()");
    await page.waitFor("!document.fullscreenElement");
    assert.equal(await page.evaluate("document.getElementById('video-fixture') !== null"), true);
  });
});
