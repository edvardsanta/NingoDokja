import test from "node:test";
import assert from "node:assert/strict";
import { observerCommands } from "../commands/observer_commands.js";
import type { DiscordCommandContext } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";

test("observer captures only consenting users and never calls reply generation",async()=>{
 const previous=globalThis.fetch;const events:any[]=[];let params:any;let stops=0;
 globalThis.fetch=async(_,init)=>{const body=JSON.parse(String(init?.body));events.push(body);return new Response('{"status":"ok","realtime":{"accepted":true}}');};
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>{stops++;return "stopped";}} as unknown as DiscordVoiceController;
 const commands=observerCommands("http://orch",1000,voice);
 const replies:string[]=[];
 const context=(id:string):DiscordCommandContext=>({userId:id,guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async m=>{replies.push(String(m));},updateReply:async()=>{},showModal:async()=>{}});
 const run=async(name:string,id:string)=>commands.find(c=>c.name===name)!.handle(context(id));
 try{
  await run("conversation_start","a");assert.deepEqual([...params.consentingUsers],["a"]);
  assert.ok(replies[0].includes("Only explicitly consenting"));
  await params.onObservation({userId:"b",sequence:0,text:"not authorized",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});
  assert.equal(events.length,1);
  await run("conversation_join","b");assert.ok(params.consentingUsers.has("b"));
  await params.onObservation({userId:"b",sequence:1,text:"authorized",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});
  assert.equal(events.at(-1).event_type,"realtime.observe");assert.equal(events.at(-1).payload.userId,"b");
  await run("conversation_leave","b");assert.equal(params.consentingUsers.has("b"),false);
  await run("conversation_stop","a");assert.equal(stops,1);assert.equal(params.consentingUsers.size,0);
  assert.ok(events.every(e=>e.event_type.startsWith("realtime.")));
 }finally{globalThis.fetch=previous;}
});
