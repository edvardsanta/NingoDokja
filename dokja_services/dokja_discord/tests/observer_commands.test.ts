import test from "node:test";
import assert from "node:assert/strict";
import { observerCommands } from "../commands/observer_commands.js";
import { createCommands } from "../commands/commands.js";
import type { DiscordCommandContext } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";

test("observer captures only consenting users and never calls reply generation",async()=>{
 const previous=globalThis.fetch;const events:any[]=[];let params:any;let stops=0;
 globalThis.fetch=async(_,init)=>{const body=JSON.parse(String(init?.body));events.push(body);return new Response('{"status":"ok","realtime":{"accepted":true}}');};
 const voice={getVoiceChannelId:async()=>"voice",startConversation:async(p:any)=>{params=p;return "started";},stopConversation:async()=>{stops++;return "stopped";}} as unknown as DiscordVoiceController;
 const commands=observerCommands("http://orch",1000,voice,"en");
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
        ? "Inicie um observador para quem consentir; Ningo fica em silêncio."
        : "Start an observer for consenting speakers; Ningo stays silent.");
      await run("conversation_start");
      const active = replies.at(-1)!;
      assert.ok(active.includes(portuguese ? "Somente participantes que consentiram explicitamente" : "Only explicitly consenting participants"));
      for (const command of ["/conversation_join", "/conversation_leave", "/conversation_stop"]) {
        assert.ok(active.includes(command));
      }
      assert.ok(active.includes(portuguese ? "15 minutos de inatividade" : "15 minutes of inactivity"));
      assert.ok(active.includes(portuguese ? "Ningo não falará" : "Ningo will not speak"));
      await run("conversation_join");
      assert.ok(replies.at(-1)!.startsWith(portuguese ? "Você consentiu" : "You consented"));
      await run("conversation_leave");
      assert.ok(replies.at(-1)!.startsWith(portuguese ? "Seu consentimento" : "Your transcription consent"));
      await run("conversation_join");
      assert.ok(replies.at(-1)!.startsWith(portuguese ? "Inicie uma nova sessão" : "Start a new observer session"));
      params.onStopped();
      assert.ok(updates.at(-1)!.startsWith(portuguese ? "Observador de conversa encerrado." : "Conversation observer stopped."));
      await run("conversation_start");
      await run("conversation_stop");
      assert.equal(replies.at(-1), portuguese
        ? "Observador de conversa encerrado; transcrições temporárias excluídas."
        : "Conversation observer stopped; ephemeral transcripts deleted.");
      await run("conversation_leave");
      assert.equal(replies.at(-1), portuguese ? "Nenhum observador está ativo" : "No observer is active");
    } finally {
      globalThis.fetch = previous;
    }
  });
}
