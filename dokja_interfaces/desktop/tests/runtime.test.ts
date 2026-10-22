import test from "node:test";
import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ACTIONS } from "../src/main/actions.js";
import { handleRequest } from "../src/main/ipc.js";
import { RuntimeClient, runtimeRoot, runtimeStateHome } from "../src/main/runtime_client.js";
import { runtimeSnapshot } from "../src/main/runtime_actions.js";
import { parseConfig } from "../src/main/config.js";

const snapshot={modules:[{name:"memory",state:"stopped",dependencies:["orchestrator","embedding_init"],private:"drop"}],busy:false,error:""};
const wrap=(body:unknown)=>({domain:"runtime",result:body});
test("runtime reads never need a gesture, changes require input and fields are allowlisted",async()=>{
 const calls:unknown[]=[];const client={async request(type:string,payload:Record<string,unknown>){calls.push({type,payload});return {status:"ok",result:wrap(snapshot)};}};
 const limits={defaultTimeoutMs:15000,maxTimeoutMs:120000};
 assert.equal((await handleRequest({type:"runtime.modules"},client,limits)).ok,true);
 assert.equal((await handleRequest({type:"runtime.set",payload:{module:"memory",enabled:true}},client,limits)).ok,false);
 assert.equal(calls.length,1);
 assert.equal((await handleRequest({type:"runtime.set",payload:{module:"memory",enabled:true,command:"erase",root:"/other"}},client,limits,undefined,{recent:()=>true})).ok,true);
 assert.deepEqual(calls[1],{type:"runtime.set",payload:{module:"memory",enabled:true}});
 for(const payload of [{module:"--all",enabled:true},{module:"embedding_init",enabled:true},{module:"memory",enabled:"yes"}])assert.equal(ACTIONS["runtime.set"].payload(payload),undefined);
 const result=ACTIONS["runtime.modules"].project(wrap(snapshot));assert.equal(JSON.stringify(result).includes("private"),false);
});
test("runtime root comes from trusted shell configuration",()=>{
 assert.equal(parseConfig(["--runtime-root=/local/checkout"],{},"en").runtimeRoot,"/local/checkout");
 assert.ok(runtimeRoot(process.cwd()));assert.equal(runtimeRoot("/nonexistent-fixture-checkout"),undefined);
});
test("remote/unconfigured control fails without launching a daemon",async()=>{
 const client=new RuntimeClient({stateHome:"/nonexistent-fixture",binary:"/not-a-controller",enabled:false});
 await assert.rejects(client.request("runtime.modules",{},100),/local checkout/);
 await assert.rejects(client.request("runtime.remove",{},100),/unsupported runtime/);
});
test("the host controller is detached, reused and project-bound across desktop sessions",{timeout:10000},async(t)=>{
 const home=await mkdtemp(join(tmpdir(),"dokja-rt-"));const binary=join(home,"fake-controller");
 // A local protocol fixture; it has no container or shell command capability.
 const source=`#!/usr/bin/env node
const fs=require('node:fs'),http=require('node:http'),crypto=require('node:crypto'),path=require('node:path');
const args=process.argv.slice(2),root=args[args.indexOf('--root')+1],state=args[args.indexOf('--state-dir')+1];
const project=crypto.createHash('sha256').update(root).digest('hex').slice(0,16);
fs.appendFileSync(path.join(state,'launches'),'1');fs.writeFileSync(path.join(state,'pid'),String(process.pid));
const server=http.createServer((req,res)=>{let body='';req.on('data',c=>body+=c);req.on('end',()=>{const e=JSON.parse(body);fs.writeFileSync(path.join(state,'last'),JSON.stringify(e));res.end(JSON.stringify({version:1,project_id:e.payload.fixtureWrongProject?'wrong':project,status:'ok',result:{domain:'runtime',result:{modules:[],busy:false}}}));});});
server.listen(path.join(state,'control.sock'));
`;
 await writeFile(binary,source);await chmod(binary,0o700);
 const legacy=join(home,"legacy");const options={root:home,stateHome:home,legacyStateHome:legacy,binary,enabled:true};const first=new RuntimeClient(options);
 await mkdir(join(legacy,`runtime-${first.projectId}`),{recursive:true});await writeFile(join(legacy,`runtime-${first.projectId}`,"ownership.json"),'{"owned":{"memory":true}}');
 t.after(async()=>{try{process.kill(Number(await readFile(join(first.stateDir,"pid"),"utf8")),"SIGTERM");}catch{}await rm(home,{recursive:true,force:true});});
 const read=await first.request("runtime.modules",{},3000);assert.equal(read.status,"ok");
 await assert.rejects(first.request("runtime.modules",{fixtureWrongProject:true},3000),/incompatible response/);
 const second=new RuntimeClient(options);await second.request("runtime.set",{module:"memory",enabled:true},3000);
 assert.equal(await readFile(join(first.stateDir,"launches"),"utf8"),"1");
 assert.equal(await readFile(join(first.stateDir,"ownership.json"),"utf8"),'{"owned":{"memory":true}}');
 const event=JSON.parse(await readFile(join(first.stateDir,"last"),"utf8"));assert.equal(event.source,"desktop");assert.equal(event.type,"runtime.set");assert.deepEqual(event.payload,{module:"memory",enabled:true});assert.ok(event.event_id);assert.ok(event.timestamp);
});

test("every local interface finds the controller in the same state directory",()=>{
 assert.equal(runtimeStateHome({XDG_STATE_HOME:"/state"},"/home/user"),"/state/dokja");
 assert.equal(runtimeStateHome({XDG_STATE_HOME:"relative"},"/home/user"),"/home/user/.local/state/dokja");
 assert.equal(runtimeStateHome({},"/home/user"),"/home/user/.local/state/dokja");
});
test("modules a newer controller adds are left out instead of breaking the card",()=>{
 const result=runtimeSnapshot({busy:false,error:"",modules:[{name:"future_module",state:"stopped"},{name:"memory",state:"ready",dependencies:[]}]});
 assert.deepEqual(result.modules.map(m=>m.name),["memory"]);
 assert.throws(()=>runtimeSnapshot({busy:false,modules:[{name:"memory",state:"exploded"}]}));
});
