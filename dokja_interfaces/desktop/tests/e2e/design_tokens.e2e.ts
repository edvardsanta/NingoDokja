// Run against the built app: CSS aliases and component roles must resolve in Chromium.
import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";

import { Page, hasDisplay, launchApp, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("design token compatibility", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;

  before(async () => {
    orchestrator = await startFakeOrchestrator();
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("document.querySelector('.composer') !== null");
    await page.evaluate(`(() => {
      const button = document.createElement('button');
      button.id = 'token-test-button';
      button.type = 'button';
      button.textContent = 'Token probe';
      const status = document.createElement('span');
      status.id = 'token-test-status';
      status.dataset.tone = 'ok';
      status.style.color = 'var(--tone-text)';
      status.textContent = 'Working';
      document.body.append(button, status);
    })()`);
  });

  after(async () => {
    page?.close();
    await app?.stop();
    await orchestrator?.stop();
  });

  const colors = () => page.evaluate<Record<string, string>>(`(() => {
    const button = getComputedStyle(document.getElementById('token-test-button'));
    return { text: button.color, border: button.borderTopColor,
      status: getComputedStyle(document.getElementById('token-test-status')).color,
      page: getComputedStyle(document.body).backgroundColor,
      ink: getComputedStyle(document.body).color };
  })()`);

  it("preserves the approved page, action and working-state colors", async () => {
    assert.deepEqual(await colors(), {
      text: "rgb(59, 232, 245)", border: "rgb(59, 232, 245)",
      status: "rgb(59, 232, 245)", page: "rgb(16, 27, 37)", ink: "rgb(214, 230, 243)",
    });
    const body = await page.evaluate<{ font: string; size: string; line: string }>(`(() => {
      const style = getComputedStyle(document.body);
      return { font: style.fontFamily, size: style.fontSize, line: style.lineHeight };
    })()`);
    assert.match(body.font, /B612 Mono/);
    assert.equal(body.size, "15px");
    assert.equal(body.line, "24px");
  });

  it("changes action roles independently of working-state and legacy cyan consumers", async () => {
    try {
      await page.evaluate("document.documentElement.style.setProperty('--action-primary', '#123456')");
      const current = await colors();
      assert.equal(current.text, "rgb(18, 52, 86)");
      assert.equal(current.border, "rgb(18, 52, 86)");
      assert.equal(current.status, "rgb(59, 232, 245)");
      assert.equal(await page.evaluate("getComputedStyle(document.documentElement).getPropertyValue('--cyan').trim()"), "#3be8f5");
    } finally {
      await page.evaluate("document.documentElement.style.removeProperty('--action-primary')");
    }
  });

  it("allows a component role override without recoloring its border or status", async () => {
    try {
      await page.evaluate("document.documentElement.style.setProperty('--button-text', '#ffb22e')");
      const current = await colors();
      assert.equal(current.text, "rgb(255, 178, 46)");
      assert.equal(current.border, "rgb(59, 232, 245)");
      assert.equal(current.status, "rgb(59, 232, 245)");
    } finally {
      await page.evaluate("document.documentElement.style.removeProperty('--button-text')");
    }
  });

  it("keeps legacy surface, typography and geometry aliases resolved", async () => {
    const aliases = await page.evaluate<Record<string, string>>(`(() => {
      const root = getComputedStyle(document.documentElement);
      return Object.fromEntries(['--static','--panel','--edge','--ink','--ink-dim','--cyan',
        '--magenta','--magenta-text','--amber','--off','--rule','--mono','--serif','--cut','--chamfer']
        .map(name => [name, root.getPropertyValue(name).trim()]));
    })()`);
    for (const [name, value] of Object.entries(aliases)) {
      assert.ok(value, `${name} must remain available`);
      assert.doesNotMatch(value, /var\(/, `${name} must resolve its dependencies`);
    }
    assert.equal(aliases['--cut'], "12px");
    assert.match(aliases['--chamfer']!, /^polygon\(/);
    assert.match(aliases['--serif']!, /Bodoni Moda/);
  });
});
