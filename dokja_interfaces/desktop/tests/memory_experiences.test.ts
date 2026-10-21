import test from "node:test";
import assert from "node:assert/strict";
import { ACTIONS } from "../src/main/actions.js";
import { handleRequest, isAllowedAction } from "../src/main/ipc.js";

const raw = {
  ref: "fixture:1", action: "hashtag.suggest", detail: "#Reading", outcome: "",
  predicted_p: 0, baseline_p: 0.5, matched_score: 0.9, context_snippet: "Context excerpt", matched_snippet: "Earlier excerpt",
  created_at: "2026-10-21T00:00:00Z", resolved_at: "", context: "Private full context", secret: "never forward",
  feedback: { code: "hashtag_tag", message: "Explicit feedback", secret: "drop" },
};
const wrap = (body: unknown) => ({ domain: "memory", result: body });
const limits = { defaultTimeoutMs: 15000, maxTimeoutMs: 120000 };

test("experience reads are allowlisted, bounded and need no write gesture", async () => {
  const calls: unknown[]=[];
  const client={async request(type: string,payload: Record<string,unknown>){calls.push({type,payload});return {status:"ok",result:wrap(type==="memory.list"?{experiences:[raw],total:7,offset:0,limit:20}:{...raw,found:true})};}};
  const list=await handleRequest({type:"memory.list",payload:{limit:1000,offset:-1,state:"pending",include_context:false,extra:"discard"}},client,limits);
  assert.ok(list.ok);
  assert.deepEqual(calls[0],{type:"memory.list",payload:{limit:20,offset:0,state:"pending",include_context:true}});
  if(!list.ok) throw new Error("read failed");
  const entry=(list.result as {experiences:Record<string,unknown>[]}).experiences[0]!;
  assert.equal(entry.predictedP,0);
  assert.equal("context" in entry,false);
  assert.equal("secret" in entry,false);
  const detail=await handleRequest({type:"memory.get",payload:{ref:"fixture:1",include_context:false,outcome:"accepted"}},client,limits);
  assert.ok(detail.ok);
  assert.deepEqual(calls[1],{type:"memory.get",payload:{ref:"fixture:1",include_context:true}});
  assert.equal(isAllowedAction("memory.resolve"),false);
  assert.equal(ACTIONS["memory.list"].payload({state:"expired"}),undefined);
  for(const ref of ["","\u0000","x".repeat(513)]) assert.equal(ACTIONS["memory.get"].payload({ref}),undefined);
});

test("experience detail preserves null probabilities and bounds context without markup handling",()=>{
  const project=(body: unknown)=>ACTIONS["memory.get"].project(wrap(body)) as {found:boolean;experience:Record<string,unknown>};
  const result=project({...raw,found:true,predicted_p:null,baseline_p:2,context:"📚".repeat(4500)});
  assert.equal(result.experience.predictedP,null);
  assert.equal(result.experience.baselineP,null);
  assert.equal(Array.from(result.experience.context as string).length,4000);
  assert.equal(result.experience.contextTruncated,true);
  assert.deepEqual(project({found:false}),{found:false});
  assert.deepEqual(ACTIONS["memory.list"].project(wrap({skipped:true,reason:"paused"})),{off:true,reason:"paused"});
});
