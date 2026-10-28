import test from "node:test";
import assert from "node:assert/strict";
import { conversationReply } from "../voice/conversation_reply.js";
import { voiceAudioRequest } from "../voice/orchestrated_audio.js";

test("voice reply forwards actual speakers and physical room, and ignores heartbeat frames",async()=>{
 const previous=globalThis.fetch;
 globalThis.fetch=async(url,init)=>{
  assert.equal(String(url),"http://orch/orchestrator/voice/chat/stream");
  const body=JSON.parse(String(init?.body));assert.equal(body.payload.userId,"second");assert.equal(body.payload.voiceChannelId,"room");assert.equal(body.payload.channelId,"text");assert.equal(body.payload.voiceTurns.length,2);assert.equal(body.payload.startSession,undefined);
  return new Response('{"heartbeat":true}\n{"status":"ok","reply":"hello"}\n');
 };
 try{assert.equal(await conversationReply("http://orch/orchestrator",1000,{guildId:"g",originChannelId:"text",turn:{userId:"second",channelId:"room",signal:new AbortController().signal,turns:[{userId:"first",text:"hi",sequence:0},{userId:"second",text:"hello",sequence:1}]}}),"hello");}finally{globalThis.fetch=previous;}
});

test("cancelled voice requests never dispatch and incomplete streams never become speech",async()=>{
 const previous=globalThis.fetch;let calls=0;
 globalThis.fetch=async()=>{calls++;return new Response('{"heartbeat":true}\n');};
 const controller=new AbortController();controller.abort();
 const input={guildId:"g",originChannelId:"text",turn:{userId:"a",channelId:"room",signal:controller.signal,turns:[{userId:"a",text:"hi",sequence:0}]}};
 try{await assert.rejects(conversationReply("http://orch",1000,input));assert.equal(calls,0);input.turn.signal=new AbortController().signal;await assert.rejects(conversationReply("http://orch",1000,input),/without a final reply/);}finally{globalThis.fetch=previous;}
});

test("synthesis uses an orchestrator event rather than a direct service request",async()=>{
 const previous=globalThis.fetch;
 globalThis.fetch=async(url,init)=>{assert.equal(url,"http://orch");const body=JSON.parse(String(init?.body));assert.equal(body.event_type,"voice.synthesize");assert.equal(body.payload.voice.text,"hello");return new Response(JSON.stringify({status:"ok",voice:{audio_bytes_b64:"test"}}));};
 try{assert.deepEqual(await voiceAudioRequest("http://orch","synthesize",{guildId:"g",userId:"u",channelId:"c",text:"hello"}),{audio_bytes_b64:"test"});}finally{globalThis.fetch=previous;}
});


test("an intentional silent decision stays silent",async()=>{
 const previous=globalThis.fetch;
 globalThis.fetch=async()=>new Response('{"status":"ok"}\n');
 try{assert.equal(await conversationReply("http://orch",1000,{guildId:"g",originChannelId:"text",turn:{userId:"a",channelId:"room",signal:new AbortController().signal,turns:[{userId:"a",text:"hi",sequence:0}]}}),"");}finally{globalThis.fetch=previous;}
});
