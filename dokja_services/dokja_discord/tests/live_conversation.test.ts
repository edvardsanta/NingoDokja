import test from "node:test";
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { LiveConversation, type VoiceTurnContext } from "../voice/live_conversation.js";

test("overlapping participants are observed in capture order after recognition completes", async () => {
  const turns: VoiceTurnContext[] = [];
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, context) { turns.push(context); }, onError(error) { throw error; } });
  const first=live.begin(), second=live.begin();
  live.finish(second, {userId:"b",text:"second"}); await delay(5); assert.equal(turns.length,0);
  live.finish(first, {userId:"a",text:"first"}); await delay(5);
  assert.deepEqual(turns[0].turns.map(t=>t.userId),["a","b"]); live.stop();
});

test("new speech never interrupts an answer and is combined into the next turn", async () => {
  const turns:VoiceTurnContext[]=[];let interrupted=0;let finishFirst!:()=>void;
  const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){interrupted++;},
    async onTurn(_,turn){turns.push(turn);turn.decide?.(true);await turn.waitForFloor?.();if(turns.length===1)await new Promise<void>(resolve=>finishFirst=resolve);},
    onError(error){throw error;}});
  live.finish(live.begin(),{userId:"a",text:"Ningo, hello"});await delay(5);
  const signal=turns[0].signal;
  live.finish(live.begin(),{userId:"a",text:"uhum"});
  live.finish(live.begin(),{userId:"b",text:"And what happens next?"});await delay(5);
  assert.equal(signal.aborted,false);assert.equal(interrupted,0);assert.equal(turns.length,1);
  finishFirst();await delay(5);
  assert.equal(turns.length,2);assert.deepEqual(turns[1].turns.map(t=>t.text),["uhum","And what happens next?"]);
  assert.equal(interrupted,0);live.stop();assert.equal(interrupted,1);
});

test("the initial playback waits for speech, then keeps the floor through the whole answer",async()=>{
 let context:VoiceTurnContext|undefined;let proceed!:()=>void;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(_,turn){context=turn;await new Promise<void>(r=>proceed=r);},onError(error){throw error;}});
 live.finish(live.begin(),{userId:"a",text:"Ningo, hello"});await delay(5);
 const speaker=live.begin();let ready=false;const floor=context!.waitForFloor!().then(()=>{ready=true;});
 await delay(5);assert.equal(ready,false);live.finish(speaker,{userId:"b",text:"one more thought"});await floor;
 const another=live.begin();await context!.waitForFloor!();assert.equal(context!.signal.aborted,false);
 live.finish(another);proceed();live.stop();
});

test("stop cancels playback, rejects waiting playback, and suppresses late recognition",async()=>{
 let context:VoiceTurnContext|undefined;let release!:()=>void;let count=0;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(_,turn){count++;context=turn;await new Promise<void>(r=>release=r);},onError(){}});
 live.finish(live.begin(),{userId:"a",text:"Ningo, hello"});await delay(5);
 const next=live.begin();const floor=context!.waitForFloor!();live.stop();await assert.rejects(floor);
 live.finish(next,{userId:"b",text:"late"});release();await delay(5);assert.equal(count,1);
});

test("unconfirmed noise and pending recognition never block playback",async()=>{
 let context:VoiceTurnContext|undefined;let release!:()=>void;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(_,turn){context=turn;await new Promise<void>(r=>release=r);},onError(){}});
 live.finish(live.begin(),{userId:"a",text:"Ningo, hello"});await delay(5);
 const keyboard=live.begin(false);live.transcribing(keyboard);await context!.waitForFloor!();assert.equal(context!.signal.aborted,false);
 live.finish(keyboard);release();live.stop();
});

test("conversation collected during playback remains bounded",async()=>{
 let finish!:()=>void;let count=0;let overflow=0;let pending:VoiceTurnContext|undefined;
 const live=new LiveConversation({channelId:"room",settleMs:1,interrupt(){},async onTurn(_,turn){count++;if(count===1)await new Promise<void>(r=>finish=r);else pending=turn;},onError(){overflow++;}});
 live.finish(live.begin(),{userId:"a",text:"Ningo, hello"});await delay(5);
 for(let i=0;i<40;i++)live.finish(live.begin(),{userId:"a",text:`part ${i}`});
 finish();await delay(5);assert.equal(pending!.turns.length,32);assert.equal(overflow,8);live.stop();
});
