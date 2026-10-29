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
