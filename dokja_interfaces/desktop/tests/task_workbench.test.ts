import assert from "node:assert/strict";
import test from "node:test";
import {runtimeTaskConfigure,runtimeTaskDiagnostics,runtimeTaskProfiles,runtimeProviders} from "../src/main/runtime_actions.js";
import {isWriteAction} from "../src/shared/actions.js";
test("task profiles preserve fallback order and strip undeclared fields",()=>{
 const profiles={lightweight:[{provider:"kimi",model:"fixture",prompt:"private"},{provider:"codex",model:"luna"}],analysis:[]};
 const saved=runtimeTaskConfigure({configuration_revision:2,profiles,command:"private"});
 assert.deepEqual(saved,{configuration_revision:2,profiles:{lightweight:[{provider:"kimi",model:"fixture"},{provider:"codex",model:"luna"}],analysis:[]}});
 assert.equal(isWriteAction("runtime.tasks.configure"),true);
 assert.equal(runtimeTaskConfigure({configuration_revision:0,profiles}),undefined);
 assert.throws(()=>runtimeTaskProfiles({configuration_revision:1,profiles:{lightweight:[]}}));
});
test("task diagnostics expose safe metadata and all four providers",()=>{
 const projected=runtimeTaskDiagnostics({active:1,queued:2,limit:2,prompt:"private",attempts:[{task_id:1,profile:"analysis",provider:"kimi",model:"fixture",duration_ms:17,outcome:"failed",reason:"raw private error",cause:"authentication_required",stage:"generation",started_at:"2026-11-11T10:00:00Z",completion:"private"}],providers:[{provider:"kimi",model:"fixture",retry_after_ms:60000,failure_reason:"usage_limit",token:"private"}]});
 assert.equal(JSON.stringify(projected).includes("private"),false);assert.equal(projected.attempts[0]?.reason,"provider_error");assert.equal(projected.providers[0]?.retryAfterMs,60000);
 const providers=runtimeProviders({providers:[{id:"codex",state:"installed"},{id:"claude_code",state:"installed"},{id:"kimi",state:"installed"},{id:"ollama",state:"reachable"}]});
 assert.equal(providers.providers.length,4);
});
