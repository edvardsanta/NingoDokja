import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, describe, it } from "node:test";

import { COMMUNICATIONS_TOKEN, Page, hasDisplay, launchApp, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("built Desktop env-file credentials", { skip: hasDisplay ? false : "no display" }, () => {
  let orchestrator: FakeOrchestrator;
  let app: RunningApp;
  let page: Page;
  let configDir: string;
  before(async () => {
    orchestrator = await startFakeOrchestrator();
    configDir = await mkdtemp(join(tmpdir(), "dokja-communications-env-"));
    const file = join(configDir, ".env.prod");
    await writeFile(file, `DOKJA_COMMUNICATIONS_TOKEN=${COMMUNICATIONS_TOKEN}\n`, { mode: 0o600 });
    app = await launchApp(orchestrator.endpoint, "", file);
    page = await Page.connect(app.cdpPort);
    await page.waitFor("typeof window.dokja?.request === 'function' ? true : null");
  });
  after(async () => {
    page?.close();
    await app?.stop();
    await orchestrator?.stop();
    if (configDir) await rm(configDir, { recursive: true, force: true });
  });
  it("authenticates from an existing file without exposing the credential to the page", async () => {
    const answer = await page.evaluate<{ ok: boolean }>("window.dokja.request('communications.channels', {})");
    assert.equal(answer.ok, true);
    const request = orchestrator.seen.find((event) => event.type === "communications.channels");
    // The fake orchestrator refuses communications unless the secret matches.
    assert.equal(request?.source, "desktop");
    const bootstrap = await page.evaluate("window.dokja.bootstrap()");
    assert.equal(JSON.stringify({ answer, bootstrap }).includes(COMMUNICATIONS_TOKEN), false);
    assert.equal(await page.evaluate("typeof process"), "undefined");
  });
});
