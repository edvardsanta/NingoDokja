import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";
import { Page, hasDisplay, launchApp, sleep, startFakeOrchestrator, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("desktop experience feedback", { skip: hasDisplay ? false : "no display" }, () => {
 let orchestrator:FakeOrchestrator, app:RunningApp, page:Page;
 before(async()=>{orchestrator=await startFakeOrchestrator();app=await launchApp(orchestrator.endpoint);page=await Page.connect(app.cdpPort);});
 after(async()=>{page?.close();await app?.stop();await orchestrator?.stop();});
 it("inspects an experience and offers controls without resolving it",async()=>{
  await page.waitFor("document.querySelectorAll('[role=tab]').length ? true : null");
  await page.evaluate("document.querySelectorAll('[role=tab]')[2].click()");
  await page.waitFor("[...document.querySelectorAll('button')].some(b=>b.textContent==='Load experiences') ? true : null");
  await page.evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent==='Load experiences').click()");
  await page.waitFor("document.querySelector('button[aria-label=\"Inspect fixture:1\"]') ? true : null");
  await page.evaluate("document.querySelector('button[aria-label=\"Inspect fixture:1\"]').click()");
  await page.waitFor("[...document.querySelectorAll('button')].some(b=>b.textContent==='Accept result') ? true : null");
  assert.equal(orchestrator.seen.some(e=>e.type==='memory.resolve'),false);
 });
 it("refuses scripted feedback without a real gesture",async()=>{
  await sleep(3600);
  const result=await page.evaluate<{ok:boolean;error:{code:string}}>("window.dokja.request('memory.resolve',{ref:'fixture:1',outcome:'accepted'})");
  assert.equal(result.ok,false);assert.equal(result.error.code,'denied');
  assert.equal(orchestrator.seen.some(e=>e.type==='memory.resolve'),false);
 });
 it("holds an explicit acceptance for native confirmation before sending it",async()=>{
  await page.click(5,5);
  await page.evaluate("(window.__feedback=false,window.dokja.request('memory.resolve',{ref:'fixture:1',outcome:'accepted'}).then(r=>window.__feedback=r),'started')");
  await sleep(1800);
  assert.equal(await page.evaluate('window.__feedback'),false);
  assert.equal(orchestrator.seen.some(e=>e.type==='memory.resolve'),false);
 });
});
