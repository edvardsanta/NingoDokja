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
