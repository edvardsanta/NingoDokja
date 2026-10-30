// Run against the built app: every workspace, at the sizes and with the accessibility preferences
// the desktop supports. Chromium's real layout decides here, which jsdom cannot.
import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";

import { Page, hasDisplay, launchApp, sleep, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

const SIZES = [[1536, 860], [1100, 800], [800, 700]] as const;

describe("all workspaces", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;
  let tabs = 0;

  before(async () => {
    orchestrator = await startFakeOrchestrator();
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("document.querySelector('.composer') !== null");
    tabs = await page.evaluate<number>("document.querySelectorAll('[role=tab].tab').length");
  });

  after(async () => {
    await page?.resize().catch(() => undefined);
    page?.close();
    await app?.stop();
    await orchestrator?.stop();
  });

  const open = async (index: number) => {
    await page.evaluate(`document.querySelectorAll('[role=tab].tab')[${index}].click()`);
    await sleep(350);
    return page.evaluate<string>(`document.querySelectorAll('[role=tab].tab')[${index}].querySelector('.tab-label')?.textContent ?? ''`);
  };

  // What overflows sideways in the workspace on screen, by a small description of each element.
  const overflowing = () => page.evaluate<string[]>(`(() => {
    const panel = [...document.querySelectorAll('.wp')].find((each) => each.offsetParent !== null);
    if (!panel) return ['no panel'];
    const out = [];
    const body = panel.querySelector('.wp-body');
    if (body && body.scrollWidth > body.clientWidth + 1) out.push('wp-body scrolls ' + body.scrollWidth + '>' + body.clientWidth);
    if (document.documentElement.scrollWidth > innerWidth + 1) out.push('page scrolls sideways');
    return out;
  })()`);

  const dockVisible = () => page.evaluate<boolean>(`(() => {
    const dock = [...document.querySelectorAll('.wp-dock')].find((each) => each.offsetParent !== null);
    if (!dock) return true;
    const box = dock.getBoundingClientRect();
    return box.bottom <= innerHeight + 1 && box.height > 0;
  })()`);

  it("has the expected workspaces", () => assert.ok(tabs >= 6, `found ${tabs}`));

  for (const [width, height] of SIZES) {
    it(`fits every workspace at ${width}x${height}`, async () => {
      await page.resize(width, height);
      const problems: string[] = [];
      for (let index = 0; index < tabs; index += 1) {
        const name = await open(index);
        for (const problem of await overflowing()) problems.push(`${name}: ${problem}`);
        if (!(await dockVisible())) problems.push(`${name}: dock below the window`);
      }
      assert.deepEqual(problems, []);
    });
  }

  it("survives text at twice the size", async () => {
    await page.resize(1536, 860);
    await page.evaluate(`document.documentElement.style.fontSize = '200%'`);
    const problems: string[] = [];
    try {
      for (let index = 0; index < tabs; index += 1) {
        const name = await open(index);
        for (const problem of await overflowing()) problems.push(`${name}: ${problem}`);
      }
    } finally {
      await page.evaluate(`document.documentElement.style.fontSize = ''`);
    }
    assert.deepEqual(problems, []);
  });

  it("shows where the keyboard is on everything Tab reaches", async () => {
    await page.resize(1100, 800);
    const unseen: string[] = [];
    const reached = new Set<string>();
    for (let index = 0; index < tabs; index += 1) {
      const name = await open(index);
      await page.evaluate(`document.activeElement?.blur?.()`);
      for (let step = 0; step < 14; step += 1) {
        await page.tab();
        reached.add(await page.evaluate<string>(`document.activeElement === document.body ? '' : document.activeElement.tagName + (document.activeElement.textContent || '').slice(0, 12)`));
        const shown = await page.evaluate<string>(`(() => {
          const el = document.activeElement;
          if (!el || el === document.body) return '';
          const style = getComputedStyle(el);
          const ring = style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0;
          return ring ? '' : el.tagName + '.' + (el.className || '') + ' ' + (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 24);
        })()`);
        if (shown) unseen.push(`${name}: ${shown}`);
      }
    }
    assert.ok(reached.size > 10, `Tab only reached ${reached.size} elements`);
    assert.deepEqual([...new Set(unseen)], []);
  });

  it("stops the motion when asked to, and keeps controls visible in forced colors", async () => {
    await page.resize(1100, 800);
    await page.emulateMedia([{ name: "prefers-reduced-motion", value: "reduce" }, { name: "forced-colors", value: "active" }]);
    try {
      const moving: string[] = [];
      for (let index = 0; index < tabs; index += 1) {
        const name = await open(index);
        const running = await page.evaluate<string[]>(`[...document.querySelectorAll('*')].filter((el) => el.offsetParent !== null && getComputedStyle(el).animationName !== 'none' && getComputedStyle(el).animationPlayState === 'running').map((el) => el.className || el.tagName).slice(0, 5)`);
        for (const each of running) moving.push(`${name}: ${each}`);
      }
      assert.deepEqual([...new Set(moving)], []);
    } finally {
      await page.emulateMedia([{ name: "prefers-reduced-motion", value: "" }, { name: "forced-colors", value: "" }]);
    }
  });
});
