import { useEffect, useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { observerSnapshot, type ObserverSnapshot } from "../../shared/realtime.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { WorkspacePanel } from "./workspace_panel.js";

// One bounded request at a time. No provider keys, audio, direct database access,
// provider invocation, or semantic inference belongs in this renderer.
export function RealtimeCard({transport,active=true}:{transport:Transport;active?:boolean}) {
 const times=useTimes();
 const t=useTranslate();const [snapshot,setSnapshot]=useState<ObserverSnapshot>({sessions:[]});
 const [selected,setSelected]=useState("");const [connected,setConnected]=useState(false);
 useEffect(()=>{
  if(!active)return;
  let stopped=false;let timer:ReturnType<typeof setTimeout>|undefined;
  const poll=async()=>{
   try{
    const reply=await transport.request("realtime.snapshot",{}, {timeoutMs:3000});
    if(stopped)return;
    if(reply.ok){setSnapshot(observerSnapshot(reply.result));setConnected(true);}
    else {setConnected(false);setSnapshot({sessions:[]});}
   }catch{if(!stopped){setConnected(false);setSnapshot({sessions:[]});}}
   finally{if(!stopped)timer=setTimeout(()=>{void poll();},1000);}
  };
  void poll();return()=>{stopped=true;clearTimeout(timer);};
 },[transport,active]);
 const session=snapshot.sessions.find(s=>s.session_id===selected)??snapshot.sessions[0];
 return <WorkspacePanel trail={[t("nav_realtime"),t("realtime_intro")]}>
  <p aria-live="polite">{t(connected?"realtime_connected":"realtime_disconnected")}</p>
  <p>{t("realtime_hint")}</p><p className="muted">{t("realtime_limits")}</p>
  {snapshot.sessions.length===0?<p>{t("realtime_empty")}</p>:<>
   <label>{t("realtime_select")} <select value={session?.session_id??""} onChange={e=>setSelected(e.target.value)}>
    {snapshot.sessions.map((s,i)=><option key={s.session_id} value={s.session_id}>{t("realtime_session",{number:i+1})}</option>)}
   </select></label>
   <h2>{t("realtime_messages")}</h2><p>{t("realtime_pending")}</p>
   {session?.messages.length===0&&<p>{t("realtime_no_messages")}</p>}
   <ol>{session?.messages.map(m=><li key={m.message_id}>
    <strong>{t("realtime_speaker",{number:session.consenting_participants.indexOf(m.speaker_id)+1})}</strong>{" "}
    <time dateTime={m.timestamp_start}>{times.clock(m.timestamp_start)}</time>
    <p>{m.text}</p>
   </li>)}</ol>
  </>}
 </WorkspacePanel>;
}
