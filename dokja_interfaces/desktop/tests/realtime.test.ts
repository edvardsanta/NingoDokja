import test from "node:test";
import assert from "node:assert/strict";
import { observerSnapshot } from "../src/shared/realtime.js";
import { ACTION_TYPES, WRITE_ACTION_TYPES } from "../src/shared/actions.js";

test("observer read is allowlisted and provisional or provider data never reaches the view",()=>{
 assert.ok(ACTION_TYPES.includes("realtime.snapshot"));assert.ok(!WRITE_ACTION_TYPES.includes("realtime.snapshot"));
 const base={message_id:"m",session_id:"s",speaker_id:"p",text:"Redis",revision:1,timestamp_start:"now",timestamp_end:"later"};
 const result=observerSnapshot({sessions:[{session_id:"s",consenting_participants:["p"],messages:[{...base,transcription_status:"partial"},{...base,transcription_status:"final",api_key:"private"}]}]});
 assert.equal(result.sessions[0]!.messages.length,1);
 assert.equal(result.sessions[0]!.messages[0]!.text,"Redis");
 assert.ok(!JSON.stringify(result).includes("private"));
 assert.throws(()=>observerSnapshot({sessions:"bad"}));
});

test("observer preserves bounded display names and accepts snapshots without names",()=>{
 const snapshot=observerSnapshot({sessions:[{messages:[
  {message_id:"named",speaker_id:"a",speaker_name:"  Jules\n",transcription_status:"final"},
  {message_id:"old",speaker_id:"b",transcription_status:"final"},
  {message_id:"long",speaker_name:"x".repeat(300),transcription_status:"final"},
 ]}]});
 assert.equal(snapshot.sessions[0]!.messages[0]!.speaker_name,"Jules");
 assert.equal(snapshot.sessions[0]!.messages[1]!.speaker_name,"");
 assert.equal(snapshot.sessions[0]!.messages[2]!.speaker_name?.length,200);
});
