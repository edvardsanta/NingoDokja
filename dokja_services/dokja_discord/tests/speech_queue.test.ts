import test from "node:test";
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { speechSegments, playSpeechQueue } from "../voice/speech_queue.js";

test("sentence segmentation keeps decimals and bounds multi-sentence synthesis",()=>{
 assert.deepEqual(speechSegments("O valor é 3.14. Outra frase!"),["O valor é 3.14.","Outra frase!"]);
 const text="word ".repeat(200);const parts=speechSegments(text);assert.ok(parts.every(p=>p.length<=280));assert.equal(parts.join(" "),text.trim());
});
test("the first sentence plays while the next sentence is still being synthesized",async()=>{
 const played:string[]=[];let second!:()=>void;let firstEnded!:()=>void;
 const job=playSpeechQueue("First sentence. Second sentence.",{
  prepare:async(text)=>{if(text.startsWith("Second"))await new Promise<void>(r=>second=r);return text;},
  play:async(text)=>{played.push(text);if(text.startsWith("First"))await new Promise<void>(r=>firstEnded=r);},
 });
 await delay(0);assert.deepEqual(played,["First sentence."]);assert.equal(typeof second,"function");
 second();firstEnded();await job;assert.deepEqual(played,["First sentence.","Second sentence."]);
});
test("cancellation while the next sentence is prepared never plays it",async()=>{
 const abort=new AbortController();const played:string[]=[];
 await assert.rejects(playSpeechQueue("First sentence. Second sentence.",{
  signal:abort.signal,prepare:async(text)=>text,play:async(text)=>{played.push(text);abort.abort();},
 }));
 assert.deepEqual(played,["First sentence."]);
});
