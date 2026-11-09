import { render, screen, waitFor, cleanup } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { RealtimeCard } from "../../src/renderer/cards/realtime.js";
import type { Transport } from "../../src/shared/transport.js";

afterEach(cleanup);
test("observer displays actual final evidence and makes semantic limitations visible",async()=>{
 const transport:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),
 request:async(type)=>{expect(type).toBe("realtime.snapshot");return {ok:true,result:{sessions:[{
  session_id:"s",consenting_participants:["a"],messages:[{message_id:"m",speaker_id:"a",text:"Refresh tokens in Redis",transcription_status:"final",timestamp_start:"2026-10-29T10:00:00Z"}],
 }]}};}};
 render(<I18nProvider locale="en"><RealtimeCard transport={transport}/></I18nProvider>);
 await screen.findByText("Refresh tokens in Redis");
 expect(screen.getByText("Participant 1")).toBeTruthy();
 const stamp = document.querySelector("time")!;
 expect(stamp.getAttribute("datetime")).toBe("2026-10-29T10:00:00Z");
 expect(stamp.textContent).toBe(new Intl.DateTimeFormat("en", {hour:"2-digit",minute:"2-digit",second:"2-digit",hourCycle:"h23"}).format(new Date("2026-10-29T10:00:00Z")));
 const hint=screen.getByText(/\/voice_chat_start talks with Ningo/).textContent;
 expect(hint).toContain("/conversation_start");expect(hint).toContain("/conversation_stop");
 // A session without a mode is an older observer: silent transcription.
 expect(screen.getByText("Transcription only: Ningo stays silent.")).toBeTruthy();
 expect(document.body.textContent).not.toContain("/conversation_join");
 expect(screen.getByText(/Semantic associations are unresolved/)).toBeTruthy();
 expect(screen.getByText(/Segment-final transcription/)).toBeTruthy();
});

test("late replies after unmount do not restart polling",async()=>{
 let complete!:(value:any)=>void;let calls=0;
 const transport:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),request:async()=>{calls++;return new Promise(r=>complete=r);}};
 const view=render(<I18nProvider locale="en"><RealtimeCard transport={transport}/></I18nProvider>);
 await waitFor(()=>expect(calls).toBe(1));view.unmount();complete({ok:true,result:{sessions:[]}});
 await Promise.resolve();expect(calls).toBe(1);
});

for (const locale of ["en", "pt"] as const) {
 test(`observer shows participant display names with a localized unnamed fallback (${locale})`, async () => {
  const transport: Transport = {preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),request:async()=>({ok:true,result:{sessions:[{
   session_id:"s",consenting_participants:["a","b"],messages:[
    {message_id:"named",speaker_id:"a",speaker_name:"Jules",text:"Named speech",transcription_status:"final"},
    {message_id:"unnamed",speaker_id:"b",speaker_name:"  ",text:"Unnamed speech",transcription_status:"final"},
   ],
  }]}})};
  render(<I18nProvider locale={locale}><RealtimeCard transport={transport}/></I18nProvider>);
  await screen.findByText("Jules");
  expect(screen.getByText(locale === "en" ? "Participant 2" : "Participante 2")).toBeTruthy();
  expect(screen.queryByText(locale === "en" ? "Participant 1" : "Participante 1")).toBeNull();
 });
}

test("observer speaks in Ningo's voice and marks its state when no session is open",async()=>{
 const up:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),request:async()=>({ok:true,result:{sessions:[]}})};
 const view=render(<I18nProvider locale="en"><RealtimeCard transport={up}/></I18nProvider>);
 expect((await screen.findByText("I listen in silence. No one has spoken yet.")).className).toBe("voice");
 expect(screen.getByText("Observer connected").getAttribute("data-tone")).toBe("ok");
 expect(screen.getByText(/\/voice_chat_start talks with Ningo/)).toBeTruthy();
 view.unmount();
 const down:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),request:async()=>({ok:false,error:{code:"unavailable",message:"down"}})};
 render(<I18nProvider locale="pt"><RealtimeCard transport={down}/></I18nProvider>);
 await screen.findByText("Daqui não ouço nada. O observador se calou.");
 expect(screen.getByText("Observador indisponível").getAttribute("data-tone")).toBe("problem");
});

test("a voice chat from /voice_chat_start is transcribed here and shown as a conversation Ningo answers",async()=>{
 const transport:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"disabled"}}),request:async()=>({ok:true,result:{sessions:[{
  mode:"voice_chat",session_id:"s",consenting_participants:["a"],messages:[{message_id:"m",speaker_id:"a",speaker_name:"Eduardo",text:"Teste de som",transcription_status:"final"}],
 }]}})};
 render(<I18nProvider locale="pt"><RealtimeCard transport={transport}/></I18nProvider>);
 await screen.findByText("Teste de som");
 expect(screen.getByText("Conversa por voz: Ningo escuta e responde.")).toBeTruthy();
 expect(screen.queryByText("Só transcrição: Ningo permanece em silêncio.")).toBeNull();
});
