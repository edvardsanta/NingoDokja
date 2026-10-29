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

  const focus = (selector: string) => page.evaluate<{ visible: boolean; color: string; background: string; width: string; offset: string; style: string }>(`(() => {
    const el = document.querySelector(${JSON.stringify(selector)});
    el.focus();
    const style = getComputedStyle(el);
    return { visible: el.matches(':focus-visible'), color: style.outlineColor,
      background: style.backgroundColor, width: style.outlineWidth,
      offset: style.outlineOffset, style: style.outlineStyle };
  })()`);

  it("retains distinct keyboard focus defaults for fields and base buttons", async () => {
    await page.press("Tab");
    const field = await focus('.composer textarea');
    assert.equal(field.visible, true);
    assert.equal(field.color, 'rgb(59, 232, 245)');
    assert.equal(field.width, '2px');
    assert.equal(field.offset, '2px');
    assert.equal(field.style, 'solid');
    const button = await focus('#token-test-button');
    assert.equal(button.visible, true);
    assert.equal(button.color, 'rgb(214, 230, 243)');
    assert.equal(button.width, '2px');
    assert.equal(button.offset, '3px');
  });

  it("changes accent focus independently of action and working-state colors", async () => {
    try {
      await page.evaluate("document.documentElement.style.setProperty('--focus-accent-ring', '#ffb22e')");
      const field = await focus('.composer textarea');
      assert.equal(field.color, 'rgb(255, 178, 46)');
      const button = await focus('#token-test-button');
      assert.equal(button.color, 'rgb(214, 230, 243)');
      const current = await colors();
      assert.equal(current.text, 'rgb(59, 232, 245)');
      assert.equal(current.status, 'rgb(59, 232, 245)');
    } finally {
      await page.evaluate("document.documentElement.style.removeProperty('--focus-accent-ring')");
    }
  });

  const motion = () => page.evaluate<Record<string, string>>(`(() => {
    const root = document.getElementById('motion-probes');
    return {
      row: getComputedStyle(root.querySelector('li')).animationName,
      rowDuration: getComputedStyle(root.querySelector('li')).animationDuration,
      marker: getComputedStyle(root.querySelector('.service-status'), '::after').animationName,
      markerDuration: getComputedStyle(root.querySelector('.service-status'), '::after').animationDuration,
      message: getComputedStyle(root.querySelector('[role=status]'), '::after').animationName,
      drawer: getComputedStyle(root.querySelector('.drawer')).animationName,
      drawerDuration: getComputedStyle(root.querySelector('.drawer')).animationDuration,
      wordmark: getComputedStyle(document.querySelector('.wordmark')).transitionDuration
    };
  })()`);

  it("keeps approved motion timing and disables motion when requested", async () => {
    try {
      await page.evaluate(`(() => {
        const root = document.createElement('div');
        root.id = 'motion-probes';
        const list = document.createElement('ul'); list.className = 'services';
        const row = document.createElement('li'); row.dataset.tone = 'progress';
        const marker = document.createElement('span'); marker.className = 'service-status'; marker.textContent = 'Starting';
        row.append(marker); list.append(row);
        const workspace = document.createElement('div'); workspace.className = 'wp';
        const message = document.createElement('p'); message.setAttribute('role', 'status'); message.textContent = 'Waiting';
        workspace.append(message);
        const drawer = document.createElement('div'); drawer.className = 'drawer';
        root.append(list, workspace, drawer); document.body.append(root);
      })()`);
      await page.emulateMedia([{ name: 'prefers-reduced-motion', value: 'no-preference' }]);
      const normal = await motion();
      assert.equal(normal.row, 'print');
      assert.equal(normal.rowDuration, '0.5s');
      assert.equal(normal.marker, 'blink');
      assert.equal(normal.markerDuration, '1s');
      assert.equal(normal.drawer, 'drawer-in');
      assert.equal(normal.drawerDuration, '0.16s');
      assert.equal(normal.wordmark, '0.45s');
      await page.evaluate("document.documentElement.style.setProperty('--motion-overlay-duration', '2s')");
      const overridden = await motion();
      assert.equal(overridden.drawerDuration, '2s');
      assert.equal(overridden.rowDuration, '0.5s');
      assert.equal(overridden.markerDuration, '1s');
      await page.emulateMedia([{ name: 'prefers-reduced-motion', value: 'reduce' }]);
      const reduced = await motion();
      for (const key of ['row', 'marker', 'message', 'drawer']) assert.equal(reduced[key], 'none', key);
      assert.equal(reduced.wordmark, '0s');
    } finally {
      await page.evaluate("document.getElementById('motion-probes')?.remove()");
      await page.evaluate("document.documentElement.style.removeProperty('--motion-overlay-duration')");
      await page.emulateMedia([]);
    }
  });

  it("keeps a visible keyboard outline under browser forced colors", async () => {
    try {
      await page.emulateMedia([{ name: 'forced-colors', value: 'active' }]);
      assert.equal(await page.evaluate("matchMedia('(forced-colors: active)').matches"), true);
      const field = await focus('.composer textarea');
      const button = await focus('#token-test-button');
      for (const control of [field, button]) {
        assert.equal(control.visible, true);
        assert.equal(control.style, 'solid');
        assert.equal(control.width, '2px');
        assert.notEqual(control.color, control.background);
        assert.notEqual(control.color, 'rgba(0, 0, 0, 0)');
      }
    } finally {
      await page.emulateMedia([]);
    }
  });
});
