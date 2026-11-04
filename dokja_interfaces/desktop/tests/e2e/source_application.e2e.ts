import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";
import { Page, hasDisplay, launchApp, startFakeOrchestrator, SOURCE_SUPERVISION_TOKEN, type FakeOrchestrator, type RunningApp } from "./support.js";

describe("source supervision in the built Desktop",{skip:hasDisplay?false:"no display"},()=>{
 let orchestrator:FakeOrchestrator,app:RunningApp,page:Page;let snapshot:Record<string,unknown>|undefined;let revision=1,plan=1,sequence=3;
 const controls:Record<string,unknown>[]=[];
 const wrap=(body:Record<string,unknown>)=>({status:"ok",result:{domain:"knowledge",result:body}});
 before(async()=>{
  orchestrator=await startFakeOrchestrator((type,payload)=>{
   if(type==="knowledge.list")return wrap({documents:[{source_id:"guide",title:"Retry guide",kind:"note"},{source_id:"api",title:"API version one",kind:"note"}],total:2,offset:0});
   if(type==="knowledge.application.prepare"||type==="knowledge.application.supervision.preview"){const candidate=(type==="knowledge.application.supervision.preview"?payload.application:payload) as Record<string,unknown>;return wrap({status:"prepared",application:{...candidate,sources:(candidate.sources as {source_id:string}[]).map((s)=>({...s,title:s.source_id==="guide"?"Retry guide":"API version one",text_version:"sha256:"+"a".repeat(64)}))}});}
   if(type==="knowledge.application.supervision.status")return payload.application_ref ? wrap(snapshot!):wrap({available:true,host_ref:"host:fixture",remaining:16,applications:[]});
   if(type==="knowledge.application.supervision.start"){
    snapshot={application_ref:"application:fixture",state:"running",reason:"",sequence,control_revision:revision,plan_revision:plan,read_attempts:1,pending:0,uncertain:false,deadline:new Date(Date.now()+60000).toISOString(),application:payload.application,controls,receipts:[{source_id:"guide",attempt_ref:"attempt:old",state_revision:1,plan_revision:1,outcome:"succeeded",confirmed:true}]};return wrap(snapshot);
   }
   if(type==="knowledge.application.supervision.control"&&snapshot){
    revision++;sequence+=10;const previous=String(snapshot.state);
    snapshot.state=payload.action==="pause"?"paused":payload.action==="cancel"?"cancelled":payload.action==="resume"?"running":previous;
    if(payload.action==="redirect"){plan++;snapshot.application=payload.application;}
    Object.assign(snapshot,{sequence,control_revision:revision,plan_revision:plan});
    controls.push({operation_ref:payload.operation_ref,actor:"desktop:operator",action:payload.action,reason:payload.reason,control_revision:revision,plan_revision:plan});return wrap(snapshot);
   }
   return undefined;
  });
  app=await launchApp(orchestrator.endpoint,"",undefined,SOURCE_SUPERVISION_TOKEN);page=await Page.connect(app.cdpPort);
 });
 after(async()=>{page?.close();await app?.stop();await orchestrator?.stop();});
 const click=async(label:string)=>{
  const point=await page.evaluate<{x:number;y:number}>(`(()=>{const node=[...document.querySelectorAll('.source-application button')].find(n=>n.textContent==='${label}');if(!node||node.disabled)throw new Error('button unavailable');node.scrollIntoView({block:'center'});const b=node.getBoundingClientRect();return {x:b.x+b.width/2,y:b.y+b.height/2};})()`);
  await page.click(point.x,point.y);
 };
 const fill=async(index:number,value:string)=>page.evaluate(`(()=>{const node=document.querySelectorAll('.source-form textarea')[${index}];Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(node,${JSON.stringify(value)});node.dispatchEvent(new Event('input',{bubbles:true}));})()`);
 it("starts through the authenticated shell and preserves original receipts while correcting the plan",async()=>{
  await page.waitFor("[...document.querySelectorAll('[role=tab]')].some(n=>n.textContent==='Library')");
  await page.evaluate("[...document.querySelectorAll('[role=tab]')].find(n=>n.textContent==='Library').click()");
  await page.waitFor("[...document.querySelectorAll('[role=tab]')].some(n=>n.textContent==='Apply')");
  await page.evaluate("[...document.querySelectorAll('[role=tab]')].find(n=>n.textContent==='Apply').click()");
  await page.waitFor("document.querySelectorAll('.source-picker input').length===2");
  await fill(0,"Use the retry guide with the API contract");await page.evaluate("document.querySelectorAll('.source-picker input').forEach(n=>n.click())");
  await click("Check selected versions");await page.waitFor("document.querySelector('.source-reviewed') !== null");
  await click("Start supervised preparation");await page.waitFor("document.querySelector('.source-progress') !== null");
  await click("Pause");await page.waitFor("[...document.querySelectorAll('.source-application button')].some(n=>n.textContent==='Resume')");
  await click("Correct direction");await fill(0,"Use API version one");await fill(2,"The previous plan assumed version two");
  await click("Check selected versions");await page.waitFor("document.querySelector('.source-reviewed') !== null");await click("Apply correction");
  await page.waitFor("document.querySelector('.source-progress')?.textContent.includes('Plan 2')");
  assert.equal(await page.evaluate("document.querySelector('.source-receipts')?.textContent.includes('Plan 1')"),true);
  assert.equal(await page.evaluate(`document.body.textContent.includes(${JSON.stringify(SOURCE_SUPERVISION_TOKEN)})`),false);
  const commands=orchestrator.seen.filter((e)=>e.type==="knowledge.application.supervision.control");assert.deepEqual(commands.map((c)=>[c.payload.action,c.payload.expected_revision]),[["pause",1],["redirect",2]]);
  for(const width of [1536,800]){
   await page.resize(width,800);
   assert.equal(await page.evaluate("(()=>{const n=document.querySelector('.source-application');return n.scrollWidth<=n.clientWidth+1&&document.documentElement.scrollWidth<=innerWidth+1;})()"),true);
  }
  await click("Resume");await page.waitFor("[...document.querySelectorAll('.source-application button')].some(n=>n.textContent==='Pause')");await click("Cancel preparation");await page.waitFor("document.querySelector('.source-progress')?.textContent.includes('Preparation cancelled')");
 });
});
