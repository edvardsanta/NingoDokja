import test from "node:test";
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { LiveConversation, type VoiceTurnContext } from "../voice/live_conversation.js";

test("overlapping speakers are batched in capture order after all transcription completes", async () => {
  const replies: VoiceTurnContext[] = [];
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, context) { replies.push(context); }, onError(error) { throw error; } });
  const first=live.begin(), second=live.begin();
  live.finish(second, {userId:"b",text:"second"});
  await delay(5);
  assert.equal(replies.length,0);
  live.finish(first, {userId:"a",text:"first"});
  await delay(5);
  assert.deepEqual(replies[0].turns.map(t=>t.userId),["a","b"]);
  assert.equal(replies[0].userId,"b");
  assert.equal(replies[0].channelId,"room");
  live.stop();
});

test("new speech cancels pending reply and stop prevents late recognition from speaking", async () => {
  let first: VoiceTurnContext | undefined;
  let replies=0, interruptions=0;
  const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){interruptions++;},
    async onTurn(_,context){ replies++; first=context; await new Promise<void>(resolve=>context.signal.addEventListener("abort",()=>resolve(),{once:true})); },
    onError(error){throw error;}});
  const a=live.begin();live.finish(a,{userId:"a",text:"hello"});await delay(5);
  assert.equal(first?.signal.aborted,false);
  const b=live.begin();assert.equal(first?.signal.aborted,true);
  live.stop();live.finish(b,{userId:"b",text:"late"});await delay(5);
  assert.equal(replies,1);assert.ok(interruptions>=3);
});

test("short/empty recognition releases the floor without a reply",async()=>{
 let replies=0;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(){replies++;},onError(){}});
 live.finish(live.begin());await delay(5);assert.equal(replies,0);live.stop();
});

test("sustained speech keeps only a bounded recent batch",async()=>{
 const replies:VoiceTurnContext[]=[];let overflow=0;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(_,turn){replies.push(turn);},onError(){overflow++;}});
 const held=live.begin();
 for(let i=0;i<40;i++){const seq=live.begin();live.finish(seq,{userId:"a",text:`part ${i}`});}
 live.finish(held);await delay(5);
 assert.equal(replies[0].turns.length,32);assert.equal(overflow,8);live.stop();
});
