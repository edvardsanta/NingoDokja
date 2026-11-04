import test from "node:test";
import assert from "node:assert/strict";
import { ACTIONS } from "../src/main/actions.js";
import { buildEnvelope, OrchestratorClient } from "../src/main/orchestrator_client.js";
import { handleRequest } from "../src/main/ipc.js";
import { sourceSupervisionToken } from "../src/main/communications_config.js";
import { sourceApplication, SOURCE_CONTRACT } from "../src/shared/source_application.js";
const pin="sha256:"+"a".repeat(64);
const application={contract:SOURCE_CONTRACT,goal:"Use the API constraints",constraints:[],sources:[{source_id:"guide",text_version:pin}]};
const wrap=(body:unknown)=>({domain:"knowledge",result:body});
function snapshot(){return {application_ref:"application:one",state:"paused",reason:"",sequence:8,control_revision:2,plan_revision:1,read_attempts:1,pending:0,uncertain:false,deadline:"2030-01-01T00:00:00Z",application,controls:[],receipts:[{source_id:"guide",attempt_ref:"attempt:old",state_revision:1,plan_revision:1,outcome:"succeeded",confirmed:true}]};}
test("source actions retain exact pins and ignore renderer authority",()=>{
 const start=ACTIONS["knowledge.application.supervision.start"].payload({host_ref:"host:one",operation_ref:"start:one",application,is_owner:true,source_supervision_token:"claimed"});
 assert.deepEqual(start,{host_ref:"host:one",operation_ref:"start:one",application});
 assert.equal(ACTIONS["knowledge.application.supervision.start"].payload({host_ref:"host:one",operation_ref:"start:one",application:{...application,sources:[{source_id:"guide"}]}}),undefined);
 assert.equal(sourceApplication({...application,goal:"x".repeat(2001)},true),undefined);
 assert.equal(sourceApplication({...application,sources:[application.sources[0],application.sources[0]]},true),undefined);
 assert.deepEqual(sourceApplication({...application,constraints:null},true)?.constraints,[]);
});
test("source projections keep native outcomes and original revisions without secret context",()=>{
 const projected=ACTIONS["knowledge.application.supervision.status"].project(wrap({...snapshot(),token:"private",context:{credential:"private"}})) as Record<string,unknown>;
 assert.equal(projected.state,"paused");assert.deepEqual(projected.receipts,[{attemptRef:"attempt:old",sourceId:"guide",controlRevision:1,planRevision:1,outcome:"succeeded",confirmed:true}]);
 assert.equal(JSON.stringify(projected).includes("private"),false);
 for(const changed of [{pending:1,state:"prepared"},{uncertain:true,state:"prepared"},{receipts:[{...snapshot().receipts[0],confirmed:undefined}]},{receipts:[{...snapshot().receipts[0],outcome:"model-done"}]}])assert.throws(()=>ACTIONS["knowledge.application.supervision.status"].project(wrap({...snapshot(),...changed})));
});
test("source controls require current revision and structured correction",()=>{
 const base={application_ref:"application:one",operation_ref:"control:one",expected_revision:2,action:"redirect",reason:"The source version is wrong",application};
 assert.deepEqual(ACTIONS["knowledge.application.supervision.control"].payload({...base,is_owner:true}),base);
 for(const change of [{expected_revision:0},{expected_revision:1.5},{reason:""},{application:undefined},{action:"execute"}])assert.equal(ACTIONS["knowledge.application.supervision.control"].payload({...base,...change}),undefined);
});
test("source writes require a shell-observed gesture; status and preview remain reads",async()=>{
 let calls=0;const client={request:async()=>{calls++;return {status:"ok",result:wrap(snapshot())};}};
 const limits={defaultTimeoutMs:1000,maxTimeoutMs:1000};
 const payload={application_ref:"application:one",operation_ref:"control:one",expected_revision:2,action:"pause"};
 const denied=await handleRequest({type:"knowledge.application.supervision.control",payload},client,limits);
 assert.equal(denied.ok,false);assert.equal(calls,0);
 const accepted=await handleRequest({type:"knowledge.application.supervision.control",payload},client,limits,undefined,{recent:()=>true});assert.equal(accepted.ok,true);assert.equal(calls,1);
 const read=await handleRequest({type:"knowledge.application.supervision.status",payload:{application_ref:"application:one"}},client,limits);assert.equal(read.ok,true);
});
test("dedicated source credential stays in the main process and only its action envelope",async()=>{
 const token="test-source-supervision-credential-000000";
 assert.equal(sourceSupervisionToken({DOKJA_SOURCE_SUPERVISION_TOKEN:token,DOKJA_COMMUNICATIONS_TOKEN:"different"}),token);
 assert.equal(sourceSupervisionToken({DOKJA_SOURCE_SUPERVISION_TOKEN:""}),"");
 const source=buildEnvelope("knowledge.application.supervision.status",{},"other",token);
 assert.equal(source.context.source_supervision_token,token);
 const ordinary=buildEnvelope("knowledge.list",{},"other",token);assert.equal(JSON.stringify(ordinary).includes(token),false);
 const client=new OrchestratorClient({endpoint:"tcp://example.com:5558",sourceSupervisionToken:token});
 await assert.rejects(client.request("knowledge.application.supervision.status",{},1000),/local endpoint/);
 const unconfigured=new OrchestratorClient({endpoint:"tcp://127.0.0.1:5558"});await assert.rejects(unconfigured.request("knowledge.application.supervision.status",{},1000),/not configured/);
});
test("preview retains domain text hashes and full supported document identifiers",()=>{
 const preview=ACTIONS["knowledge.application.prepare"].project(wrap({status:"prepared",application:{...application,sources:[{...application.sources[0],title:"Guide",text:"not exposed"}]}}));
 assert.equal(JSON.stringify(preview).includes("not exposed"),false);assert.equal(JSON.stringify(preview).includes(pin),true);
 const id="n".repeat(230);const page=ACTIONS["knowledge.list"].project(wrap({documents:[{source_id:id}],total:1,offset:0})) as {documents:{id:string}[]};assert.equal(page.documents[0]!.id,id);
});
