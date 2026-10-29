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
 const context=(id:string):DiscordCommandContext=>({userId:id,guildId:"g",channelId:"text",messageId:"request",getString:()=>"",getNumber:()=>undefined,reply:async m=>{replies.push(String(m));},updateReply:async()=>{},showModal:async()=>{}});
 const run=async(name:string,id:string)=>commands.find(c=>c.name===name)!.handle(context(id));
 try{
  await run("conversation_start","a");assert.equal(params.consentingUsers,undefined);
  assert.deepEqual(commands.map(c=>c.name),["conversation_start","conversation_stop"]);
  assert.equal(replies[0], "Transcription started.");
  await params.onObservation({userId:"b",sequence:0,text:"second speaker",startedAt:new Date().toISOString(),endedAt:new Date().toISOString()});
  assert.equal(events.at(-1).event_type,"realtime.observe");assert.equal(events.at(-1).payload.userId,"b");
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
