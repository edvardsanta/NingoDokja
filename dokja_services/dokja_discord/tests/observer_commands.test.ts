import test from "node:test";
import assert from "node:assert/strict";
import { observerCommands } from "../commands/observer_commands.js";
import { createCommands } from "../commands/commands.js";
import type { DiscordCommandContext } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";

test("observer transcribes channel speakers with start and stop and never generates replies",async()=>{
 const previous=globalThis.fetch;const events:any[]=[];let params:any;let stops=0;
 globalThis.fetch=async(_,init)=>{const body=JSON.parse(String(init?.body));events.push(body);return new Response('{"status":"ok","realtime":{"accepted":true}}');};
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>{stops++;return "stopped";}} as unknown as DiscordVoiceController;
 const commands=observerCommands("http://orch",1000,voice,"en");
 const replies:string[]=[];
 const context=(id:string):DiscordCommandContext=>({userId:id,userName:"Starter name",guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async m=>{replies.push(String(m));},updateReply:async()=>{},showModal:async()=>{}});
 const run=async(name:string,id:string)=>commands.find(c=>c.name===name)!.handle(context(id));
 try{
  await run("conversation_start","a");assert.equal(params.consentingUsers,undefined);
  assert.deepEqual(commands.map(c=>c.name),["conversation_start","conversation_stop"]);
  assert.equal(replies[0], "Transcription started.");
  await params.onObservation({userId:"b",userName:"Channel Scholar",sequence:0,text:"second speaker",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});
  assert.equal(events.at(-1).event_type,"realtime.observe");assert.equal(events.at(-1).payload.userId,"b");assert.equal(events.at(-1).payload.userName,"Channel Scholar");
  await run("conversation_stop","b");assert.equal(stops,1);
  const count=events.length;
  await params.onObservation({userId:"a",sequence:1,text:"late",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});
  assert.equal(events.length,count);
  assert.ok(events.every(e=>e.event_type.startsWith("realtime.")));
 }finally{globalThis.fetch=previous;}
});

// Exercise the factory used by the app so dropping options.locale is a regression.

for (const locale of [undefined, "pt-BR", "en", "en-US", "fr"]) {
  test(`observer lifecycle follows configured locale ${locale ?? "default"}`, async () => {
    const previous = globalThis.fetch;
    globalThis.fetch = async () => new Response('{"realtime":{"accepted":true}}');
    const portuguese = locale === undefined || locale === "pt-BR";
    let params: any;
    const replies: string[] = [];
    const updates: string[] = [];
    const voice = {
      getVoiceChannelId: async () => "voice",
      startConversation: async (value: any) => { params = value; return "started"; },
      stopConversation: async () => "stopped",
    } as unknown as DiscordVoiceController;
    const commands = createCommands("http://orch", 1000, voice, { locale });
    const context: DiscordCommandContext = {
      userId: "a", guildId: "g", channelId: "text", messageId: "request",
      getString: () => "", getNumber: () => undefined,
      reply: async message => { replies.push(String(message)); },
      updateReply: async message => { updates.push(String(message)); },
      showModal: async () => {},
    };
    const run = async (name: string) => commands.find(command => command.name === name)!.handle(context);
    try {
      const start = commands.find(command => command.name === "conversation_start")!;
      assert.equal(start.description, portuguese
        ? "Inicie a transcrição do canal de voz; Ningo fica em silêncio."
        : "Start transcribing the voice channel; Ningo stays silent.");
      await run("conversation_start");
      const active = replies.at(-1)!;
      assert.equal(active, portuguese ? "Transcrição iniciada." : "Transcription started.");
      params.onStopped();
      assert.ok(updates.at(-1)!.startsWith(portuguese ? "Observador de conversa encerrado." : "Conversation observer stopped."));
      await run("conversation_start");
      await run("conversation_stop");
      assert.equal(replies.at(-1), portuguese
        ? "Observador de conversa encerrado; transcrições temporárias excluídas."
        : "Conversation observer stopped; ephemeral transcripts deleted.");
      await run("conversation_stop");
      assert.equal(replies.at(-1), portuguese ? "Nenhum observador está ativo" : "No observer is active");
    } finally {
      globalThis.fetch = previous;
    }
  });
}

test("voice_chat_start mirrors what is said into a voice_chat realtime journal and ends it on stop",async()=>{
 const previous=globalThis.fetch;const events:any[]=[];let params:any;
 globalThis.fetch=async(_,init)=>{const body=JSON.parse(String(init?.body));events.push(body);return new Response('{"status":"ok","realtime":{"accepted":true}}');};
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>"stopped"} as unknown as DiscordVoiceController;
 const commands=createCommands("http://orch",1000,voice,{locale:"en"});
 const context:DiscordCommandContext={userId:"a",userName:"Starter",guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async()=>{},updateReply:async()=>{},showModal:async()=>{}};
 const settle=async()=>{for(let i=0;i<20;i++)await new Promise(resolve=>setImmediate(resolve));};
 try{
  await commands.find(c=>c.name==="voice_chat_start")!.handle(context);await settle();
  assert.equal(events[0].event_type,"realtime.start");assert.equal(events[0].payload.realtime.mode,"voice_chat");assert.equal(events[0].payload.realtime.voice_channel_id,"voice");
  assert.equal(params.activationMode,"always");assert.equal(params.onObservation,undefined,"Ningo still answers a voice chat");
  params.onHeard({userId:"b",userName:"Scholar",sequence:3,text:"what did Malthus fear?",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});await settle();
  const observe=events.find(e=>e.event_type==="realtime.observe");
  assert.equal(observe.payload.userId,"b");assert.equal(observe.payload.realtime.text,"what did Malthus fear?");assert.equal(observe.payload.realtime.session_id,events[0].payload.realtime.session_id);
  params.onStopped();await settle();
  assert.equal(events.at(-1).event_type,"realtime.stop");
  const count=events.length;params.onHeard({userId:"b",sequence:4,text:"late",startedAt:"",endedAt:""});await settle();
  assert.equal(events.length,count,"nothing is mirrored after the voice chat stopped");
 }finally{globalThis.fetch=previous;}
});

test("voice_chat_start still starts when the realtime mirror is refused",async()=>{
 const previous=globalThis.fetch;let params:any;
 globalThis.fetch=async()=>new Response('{"status":"ok","realtime":{"accepted":false}}');
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>"stopped"} as unknown as DiscordVoiceController;
 const commands=createCommands("http://orch",1000,voice,{locale:"en"});
 const context:DiscordCommandContext={userId:"a",guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async()=>{},updateReply:async()=>{},showModal:async()=>{}};
 const errors=console.error;console.error=()=>{};
 try{
  await commands.find(c=>c.name==="voice_chat_start")!.handle(context);
  for(let i=0;i<20;i++)await new Promise(resolve=>setImmediate(resolve));
  assert.ok(params,"the voice chat started");assert.equal(params.onHeard,undefined);
 }finally{globalThis.fetch=previous;console.error=errors;}
});

test("voice_chat_start shows Ningo's spoken reply as one line revised sentence by sentence, after the question",async()=>{
 const previous=globalThis.fetch;const events:any[]=[];let params:any;const spoken:string[]=[];
 globalThis.fetch=async(url,init)=>{
  if(String(url).endsWith("/voice/chat/stream"))return new Response(['{"accepted":true}','{"speech":"Nenhum."}','{"speech":"A perfeição é para quem não governa."}','{"status":"ok","reply":""}'].join("\n")+"\n");
  const body=JSON.parse(String(init?.body));events.push(body);return new Response('{"status":"ok","realtime":{"accepted":true}}');
 };
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>"stopped",
  speakText:async(p:any)=>{spoken.push(p.text);return "";}} as unknown as DiscordVoiceController;
 const commands=createCommands("http://orch",1000,voice,{locale:"en"});
 const context:DiscordCommandContext={userId:"a",userName:"Starter",guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async()=>{},updateReply:async()=>{},showModal:async()=>{}};
 const settle=async()=>{for(let i=0;i<30;i++)await new Promise(resolve=>setImmediate(resolve));};
 try{
  await commands.find(c=>c.name==="voice_chat_start")!.handle(context);await settle();
  await params.onTurn("qual é o sistema de governo perfeito?",{userId:"b",userName:"draco",channelId:"voice",signal:new AbortController().signal,
   turns:[{userId:"b",userName:"draco",text:"qual é o sistema de governo perfeito?",sequence:7}]});
  await settle();
  assert.deepEqual(spoken,["Nenhum.","A perfeição é para quem não governa."]);
  const lines=events.filter(e=>e.event_type==="realtime.observe").map(e=>e.payload.realtime);
  assert.equal(lines.length,2);
  assert.ok(lines.every(l=>l.speaker_role==="ningo"&&l.capture_sequence===7&&l.message_id===lines[0].message_id));
  assert.deepEqual(lines.map(l=>[l.revision,l.text]),[[1,"Nenhum."],[2,"Nenhum. A perfeição é para quem não governa."]]);
 }finally{globalThis.fetch=previous;}
});
