// A smoke test of the built app: the real Electron shell, the real page, a fake orchestrator.
// It checks the security properties at run time, which no unit test can: what the page can reach,
// what it can load and what the shell lets through. Run it with `pnpm test:e2e` (it builds first)
// on a machine with a display; without one it is skipped.
import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";

import {
  HOSTILE_TITLE,
  Page,
  hasDisplay,
  launchApp,
  sleep,
  startFakeOrchestrator,
  startTripwire,
  type FakeOrchestrator,
  type RunningApp,
  type Tripwire,
} from "./support.js";

describe("the built app", { skip: hasDisplay ? false : "no display (set WAYLAND_DISPLAY or DISPLAY)" }, () => {
  let orchestrator: FakeOrchestrator;
  let tripwire: Tripwire;
  let app: RunningApp;
  let page: Page;

  before(async () => {
    orchestrator = await startFakeOrchestrator();
    tripwire = await startTripwire();
    app = await launchApp(orchestrator.endpoint);
    page = await Page.connect(app.cdpPort);
  });

  after(async () => {
    page?.close();
    await app?.stop();
    await orchestrator?.stop();
    await tripwire?.stop();
  });

  const ask = (type: string, payload: Record<string, unknown> = {}) =>
    page.evaluate<unknown>(`window.dokja.request(${JSON.stringify(type)}, ${JSON.stringify(payload)})`);

  it("shows the health card from what the orchestrator reported", async () => {
    const rows = await page.waitFor<string[]>(
      "(() => { const rows = [...document.querySelectorAll('.services li')].map((row) => row.textContent); return rows.length ? rows : null; })()",
    );
    assert.ok(rows.some((row) => row.startsWith("meme")), JSON.stringify(rows));
    assert.ok(rows.some((row) => row.startsWith("feeds") && row.includes("no plugins enabled")), JSON.stringify(rows));
  });

  it("asks the orchestrator as the desktop, and only for the tab that is open", async () => {
    assert.deepEqual(orchestrator.seen.map((event) => event.type), ["ningo.status"]);
    assert.ok(orchestrator.seen.every((event) => event.source === "desktop"));
  });

  it("gives the page no Node and no other door than three functions", async () => {
    assert.equal(
      await page.evaluate("[typeof require, typeof process, typeof module, typeof Buffer, typeof __dirname].join()"),
      "undefined,undefined,undefined,undefined,undefined",
    );
    assert.equal(await page.evaluate("Object.keys(window.dokja).sort().join()"), "bootstrap,preview,request");
    assert.equal(await page.evaluate("[typeof window.electron, typeof window.ipcRenderer].join()"), "undefined,undefined");
  });

  it("carries a content security policy with no inline scripts and no network", async () => {
    const policy = await page.evaluate<string>("document.querySelector('meta[http-equiv=\"Content-Security-Policy\"]')?.content ?? ''");
    assert.match(policy, /default-src 'none'/);
    assert.match(policy, /script-src 'self'/);
    assert.doesNotMatch(policy, /unsafe-inline|unsafe-eval|connect-src/);
  });

  it("cannot be sent to another page or open a window", async () => {
    assert.equal(await page.evaluate("window.open('https://example.com/') === null"), true);
    await page.evaluate("(location.href = 'https://example.com/', 'sent')");
    await sleep(800);
    assert.equal(await page.evaluate("location.protocol"), "file:");
  });

  it("denies every permission it is asked for", async () => {
    assert.equal(await page.evaluate("Notification.requestPermission()"), "denied");
    assert.equal(
      await page.evaluate("new Promise((resolve) => navigator.geolocation.getCurrentPosition(() => resolve('granted'), (error) => resolve(error.code)))"),
      1,
    );
    assert.equal(
      await page.evaluate("navigator.mediaDevices.getUserMedia({ audio: true }).then(() => 'granted', (error) => error.name)"),
      "NotAllowedError",
    );
  });

  it("makes no request of its own to the network", async () => {
    const outcome = await page.evaluate<string>(`fetch(${JSON.stringify(`${tripwire.origin}/fetch`)}).then(() => 'loaded', () => 'blocked')`);
    await page.evaluate(`(new Image().src = ${JSON.stringify(`${tripwire.origin}/image.png`)}, 'started')`);
    await page.evaluate(
      `(() => { const script = document.createElement('script'); script.src = ${JSON.stringify(`${tripwire.origin}/script.js`)}; document.head.append(script); return 'started'; })()`,
    );
    await sleep(1000);
    assert.equal(outcome, "blocked");
    assert.deepEqual(tripwire.hits, []);
  });

  it("refuses an action that is not on the allow-list, before the orchestrator hears of it", async () => {
    for (const type of [
      "services.set", "discord.send", "digest.refresh", "scheduler.jobs.set",
      "knowledge.delete", "knowledge.reindex", "memory.record", "memory.forget", "message.created",
    ]) {
      const answer = (await ask(type, { name: "feeds", enabled: false })) as { ok: boolean; error?: { code: string } };
      assert.equal(answer.ok, false, type);
      assert.equal(answer.error?.code, "denied", type);
    }
    assert.ok(
      !orchestrator.seen.some((event) =>
        /^(services|discord|scheduler)\.|^digest\.refresh$|^knowledge\.(delete|reindex)$|^memory\.(record|forget)$|^message\./.test(event.type),
      ),
    );
  });

  describe("a change", () => {
    const note = {
      mode: "note", title: "A thought", body: "Something to keep.",
      kind: "idea", reference: "Book, p. 12", id: "my:thought", source: "plugin:thing", source_id: "other:document",
    };
    const ingested = () => orchestrator.seen.filter((event) => event.type === "knowledge.ingest");

    it("is refused when nothing real was pressed, even if a script clicks", async () => {
      await page.evaluate("document.querySelectorAll('[role=tab]')[0].click()");
      const answer = (await ask("knowledge.ingest", note)) as { ok: boolean; error?: { code: string; message: string } };
      assert.equal(answer.ok, false);
      assert.equal(answer.error?.code, "denied");
      assert.match(answer.error?.message ?? "", /click or a key press/);
      assert.equal(ingested().length, 0, "the orchestrator never heard of it");
    });

    it("goes through right after a real click, with the payload the shell built", async () => {
      await page.click(5, 5);
      const answer = (await ask("knowledge.ingest", note)) as { ok: boolean; result?: Record<string, unknown> };
      assert.equal(answer.ok, true);
      assert.deepEqual(answer.result, { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 2, degraded: true, reason: "no embedding server is configured" });

      const [sent] = ingested();
      assert.equal(sent?.source, "desktop");
      assert.equal(sent?.payload.kind, "idea", "the person chooses the kind");
      assert.equal(sent?.payload.source_ref, "Book, p. 12", "and the reference");
      assert.equal(sent?.payload.source_id, "my:thought", "and the id");
      assert.equal(sent?.payload.source, undefined, "but the service's own source field is never taken from the page");
    });

    it("is refused again when the click was a while ago", async () => {
      await sleep(3600);
      const answer = (await ask("knowledge.ingest", note)) as { ok: boolean; error?: { code: string } };
      assert.equal(answer.ok, false);
      assert.equal(answer.error?.code, "denied");
      assert.equal(ingested().length, 1);
    });

    it("goes through right after a real key press too", async () => {
      await page.press("a");
      const answer = (await ask("knowledge.ingest", { ...note, title: "Another thought" })) as { ok: boolean };
      assert.equal(answer.ok, true);
      assert.equal(ingested().length, 2);
    });
  });

  it("hands the screen only what a card reads", async () => {
    const status = JSON.stringify(await ask("ningo.status"));
    assert.match(status, /"ok":true/);
    assert.doesNotMatch(status, /channel-1|base-url-1|hint-1|profile-1/);

    const items = JSON.stringify(await ask("digest.items", { limit: 8, per_source: 10, max_age_hours: 9999 }));
    assert.match(items, /A plain title/);
    assert.doesNotMatch(items, /example\.com|source-1|entry-1/, "no address and no plugin name");
  });

  it("fetches a picture only at an address the orchestrator listed", async () => {
    // A public address nobody listed: it must be refused at once, with no attempt to reach it.
    const started = Date.now();
    const answer = (await page.evaluate<unknown>("window.dokja.preview('https://example.com/secret.png')")) as {
      ok: boolean;
      error?: { code: string };
    };
    assert.equal(answer.ok, false);
    assert.equal(answer.error?.code, "denied");
    assert.ok(Date.now() - started < 2000, "a refused address must not wait on the network");
  });

  it("shows the digest as text, never as markup", async () => {
    await page.evaluate("document.querySelectorAll('[role=tab]')[1].click()");
    const titles = await page.waitFor<string[]>(
      "(() => { const titles = [...document.querySelectorAll('.digest li .digest-title')].map((node) => node.textContent); return titles.length ? titles : null; })()",
    );
    assert.deepEqual(titles, [HOSTILE_TITLE, "A plain title"]);
    assert.equal(await page.evaluate("document.querySelectorAll('.digest img, .digest a, .digest [href]').length"), 0);
    assert.equal(await page.evaluate("window.__owned === undefined"), true);
    assert.ok(orchestrator.seen.some((event) => event.type === "digest.status"));
  });
});
