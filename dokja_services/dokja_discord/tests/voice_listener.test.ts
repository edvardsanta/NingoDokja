import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { setTimeout as delay } from "node:timers/promises";
import { createListeningSession, VOICE_SEGMENT_BYTES, VOICE_SILENCE_MS } from "../voice/voice.js";
import type { VoiceTurnContext } from "../voice/live_conversation.js";

test("listener captures every human independently, ignores bots, and stops outstanding work",async()=>{
 const previous=globalThis.fetch;
 const speaking=new EventEmitter();const streams=new Map<string,PassThrough>();
 const replies:VoiceTurnContext[]=[];const requests:any[]=[];
 globalThis.fetch=async(_url,init)=>{
  const body=JSON.parse(String(init?.body));requests.push(body);
  return new Response(JSON.stringify({status:"ok",voice:{text:`speech ${body.payload.userId}`}}));
 };
 const session:any={channelId:"room",player:{stop(){},state:{status:"idle"}},stream:{stop(){}},
  connection:{receiver:{speaking,subscribe(id:string,options:any){assert.equal(options.end.duration,VOICE_SILENCE_MS);const stream=new PassThrough();streams.set(id,stream);return stream;}}}};
 const listener=createListeningSession({session,guildId:"g",userId:"a",originChannelId:"text",orchestratorEndpoint:"http://orch",
  participant(id){return id==="bot"?undefined:{userId:id,userName:id.toUpperCase()};},decoderFactory:()=>new PassThrough(),
  activationMode:"always",logTranscripts:false,async onTurn(_,turn){replies.push(turn!);}});
 try{
  speaking.emit("start","bot");assert.equal(streams.size,0);
  speaking.emit("start","a");speaking.emit("start","b");assert.equal(streams.size,2);
  streams.get("b")!.end(Buffer.alloc(24000));await delay(10);assert.equal(replies.length,0);
  streams.get("a")!.end(Buffer.alloc(24000));await delay(120);
  assert.deepEqual(replies[0].turns.map(t=>t.userId),["a","b"]);
  assert.ok(requests.every(r=>r.payload.channelId==="text"));
  assert.ok(requests.every(r=>r.event_type==="voice.transcribe"));
  listener.stop();speaking.emit("start","c");assert.equal(streams.size,2);
 }finally{listener.stop();globalThis.fetch=previous;}
});

test("long continuous speech is transcribed in bounded segments before its speaker stops",async()=>{
 const previous=globalThis.fetch;const speaking=new EventEmitter();let stream!:PassThrough;const sizes:number[]=[];let replies=0;
 globalThis.fetch=async(_,init)=>{const body=JSON.parse(String(init?.body));sizes.push(Buffer.from(body.payload.voice.audio_bytes_b64,"base64").length);return new Response(JSON.stringify({status:"ok",voice:{text:"part"}}));};
 const session:any={channelId:"room",player:{stop(){}},stream:{stop(){}},connection:{receiver:{speaking,subscribe(){return stream=new PassThrough();}}}};
 const listener=createListeningSession({session,guildId:"g",userId:"a",originChannelId:"text",orchestratorEndpoint:"http://orch",participant(id){return{userId:id};},decoderFactory:()=>new PassThrough(),activationMode:"always",logTranscripts:false,async onTurn(){replies++;}});
 try{speaking.emit("start","a");for(let i=0;i<VOICE_SEGMENT_BYTES/3840;i++)stream.write(Buffer.alloc(3840));await delay(10);assert.equal(sizes.length,1);assert.equal(sizes[0],VOICE_SEGMENT_BYTES+44);assert.equal(replies,0);stream.end(Buffer.alloc(24000));await delay(120);assert.equal(replies,1);}finally{listener.stop();globalThis.fetch=previous;}
});
