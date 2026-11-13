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


test("recognition timings follow the response without changing dispatch", async () => {
 const events: Array<{traceId:string;stage:string}> = []; let traceId: string | undefined;
 const live = new LiveConversation({channelId:"room", settleMs:1, interrupt(){},
 onTiming(e){events.push(e);}, onError(e){throw e;},
 async onTurn(_,turn){traceId=turn.trace?.id;await turn.waitForFloor?.();}});
 const seq=live.begin(false);live.transcribing(seq);live.recognitionStarted(seq);
 live.finish(seq,{userId:"person",text:"Ningo, question"});await delay(20);
 assert.deepEqual(events.map(e=>e.stage),["capture_started","recognition_queued",
 "recognition_started","transcript_ready","response_dispatched","floor_wait_started",
 "floor_granted","response_handler_finished"]);
 assert.ok(events.every(e=>e.traceId===traceId));live.stop();
});


test("reply preparation overlaps later speech but playback waits for quiet", async () => {
 let prepared = false; let played = false; let release!: () => void;
 const live = new LiveConversation({channelId:"room",settleMs:1,interrupt(){},onError(e){throw e;},
 async onTurn(_,turn){prepared=true;await turn.waitForFloor?.();played=true;await new Promise<void>(r=>release=r);}});
 const question=live.begin();
 const later=live.begin();
 live.finish(question,{userId:"a",text:"Ningo, explain curiosity"});
 await delay(20);
 assert.equal(prepared,true,"later speech blocked model preparation");
 assert.equal(played,false,"playback interrupted the later speaker");
 live.finish(later);
 await delay(10);assert.equal(played,true);
 release();live.stop();
});


test("later recognition cannot erase a scheduled finalized reply", async () => {
 let prepared=false;
 const live=new LiveConversation({channelId:"room",settleMs:10,interrupt(){},
 async onTurn(){prepared=true;},onError(e){throw e;}});
 live.finish(live.begin(),{userId:"a",text:"Ningo, question"});
 const later=live.begin();live.transcribing(later);
 await delay(30);assert.equal(prepared,true);
 live.finish(later);live.stop();
});

// An answer made for what was said is not spoken once the room has moved on; the batch is asked again
// with what was said since, so the reply covers the present and not the point people left.
test("an answer is not spoken when the room moved on, and is asked again with what was said since", async () => {
  const dispatches: VoiceTurnContext[] = [];
  const spoken: string[] = [];
  let release!: () => void;
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, turn) {
      dispatches.push(turn);
      if (dispatches.length === 1) await new Promise<void>(resolve => release = resolve); // the model is still thinking
      await turn.waitForFloor?.();
      spoken.push(turn.turns.map(t => t.text).join(" | "));
    }, onError(error) { throw error; } });
  live.finish(live.begin(), { userId: "a", text: "Ningo, olha essa promoção de HD" }); await delay(5);
  // While the model thinks, the conversation changes subject.
  live.finish(live.begin(), { userId: "b", text: "a marca de vídeo pode ser que baixe o preço das placas" });
  live.finish(live.begin(), { userId: "b", text: "mas as ações caíram e o preço não vai baixar de um dia para o outro" });
  release(); await delay(20);
  assert.equal(dispatches.length, 2, "the batch was asked again");
  assert.deepEqual(spoken, ["Ningo, olha essa promoção de HD | a marca de vídeo pode ser que baixe o preço das placas | mas as ações caíram e o preço não vai baixar de um dia para o outro"],
    "only the answer for the whole conversation was spoken, never the one for the old point alone");
  assert.equal(dispatches[0].signal.aborted, true);
  live.stop();
});

test("filler and stray words do not make an answer stale", async () => {
  const batches: string[] = []; const aborted: boolean[] = []; let release!: () => void;
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, turn) {
      batches.push(turn.turns.map(t => t.text).join(" | "));
      if (batches.length === 1) await new Promise<void>(r => release = r);
      await turn.waitForFloor?.(); aborted.push(turn.signal.aborted);
    }, onError(error) { throw error; } });
  live.finish(live.begin(), { userId: "a", text: "Ningo, olha essa promoção de HD" }); await delay(5);
  live.finish(live.begin(), { userId: "b", text: "Obrigado." });
  live.finish(live.begin(), { userId: "b", text: "Obrigado." });
  live.finish(live.begin(), { userId: "b", text: "uhum" });
  release(); await delay(20);
  // The first answer was spoken as it was; the filler is only the start of the next batch.
  assert.equal(batches[0], "Ningo, olha essa promoção de HD");
  assert.equal(aborted[0], false, "filler made the first answer stale");
  assert.deepEqual(batches.slice(1), ["Obrigado. | Obrigado. | uhum"]); live.stop();
});

test("an answer that has begun is never replaced, even if the room keeps talking", async () => {
  let context: VoiceTurnContext | undefined; let dispatches = 0; let proceed!: () => void;
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, turn) { dispatches++; context = turn; await turn.waitForFloor?.(); await new Promise<void>(r => proceed = r); },
    onError(error) { throw error; } });
  live.finish(live.begin(), { userId: "a", text: "Ningo, olha essa promoção de HD" }); await delay(5);
  // The floor was granted and the answer is being spoken.
  live.finish(live.begin(), { userId: "b", text: "outra coisa completamente diferente sobre carros e motores" });
  live.finish(live.begin(), { userId: "b", text: "e mais uma frase longa sobre o tempo e a chuva de ontem" });
  await context!.waitForFloor!(); await delay(5);
  assert.equal(context!.signal.aborted, false); assert.equal(dispatches, 1);
  proceed(); await delay(5); live.stop();
});

test("a batch is asked again at most twice, then answered whatever the room did", async () => {
  const outcomes: string[] = []; let dispatches = 0;
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, turn) {
      dispatches++;
      // While the first three requests are made, the room keeps saying something that matters.
      if (dispatches <= 3) live.finish(live.begin(), { userId: "b", text: `uma frase nova bem longa numero ${dispatches} sobre outro assunto` });
      try { await turn.waitForFloor?.(); outcomes.push("spoke"); } catch { outcomes.push("asked again"); }
    }, onError(error) { throw error; } });
  live.finish(live.begin(), { userId: "a", text: "Ningo, olha essa promoção de HD" }); await delay(80);
  assert.deepEqual(outcomes.slice(0, 3), ["asked again", "asked again", "spoke"], "two repeats and then the answer, though the room never stopped");
  live.stop();
});

test("an answer is not stale when the new speech was already in the batch", async () => {
  let dispatches = 0; let spoken = 0;
  const live = new LiveConversation({ channelId: "room", settleMs: 1, interrupt() {},
    async onTurn(_, turn) { dispatches++; await turn.waitForFloor?.(); spoken++; }, onError(error) { throw error; } });
  const first = live.begin(), second = live.begin();
  live.finish(first, { userId: "a", text: "Ningo, olha essa promoção de HD" });
  live.finish(second, { userId: "b", text: "a marca de vídeo pode ser que baixe o preço das placas" });
  await delay(20);
  assert.equal(dispatches, 1); assert.equal(spoken, 1); live.stop();
});
