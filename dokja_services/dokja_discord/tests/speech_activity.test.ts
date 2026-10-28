import test from "node:test";
import assert from "node:assert/strict";
import { SpeechActivity } from "../voice/speech_activity.js";
function frame(loud:boolean){const b=Buffer.alloc(3840);if(loud)for(let i=0;i<b.length;i+=2)b.writeInt16LE(i%4?1000:-1000,i);return b;}
test("quiet frames and isolated keyboard impulses do not spend recognition work",()=>{
 const quiet=new SpeechActivity();for(let i=0;i<60;i++)quiet.push(frame(false));assert.equal(quiet.transcribable,false);
 const clicks=new SpeechActivity();for(let i=0;i<30;i++)clicks.push(frame(i%5===0));assert.equal(clicks.transcribable,false);assert.equal(clicks.sustained,false);
});
test("sustained voiced activity may pause playback but acoustic detection does not cancel it",()=>{
 const speech=new SpeechActivity();for(let i=0;i<8;i++)speech.push(frame(true));assert.equal(speech.transcribable,true);assert.equal(speech.sustained,false);
 for(let i=0;i<20;i++)speech.push(frame(true));assert.equal(speech.sustained,true);
});
