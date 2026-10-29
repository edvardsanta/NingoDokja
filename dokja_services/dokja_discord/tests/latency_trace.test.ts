import test from "node:test";
import assert from "node:assert/strict";
import { VoiceLatencyTrace, prepareSpeech, type VoiceTiming } from "../voice/latency_trace.js";
test("timings are monotonic, correlated and contain no conversation data", () => {
 const events: VoiceTiming[] = []; let clock = 100;
 const trace = new VoiceLatencyTrace(e => events.push(e), () => clock);
 clock = 125; trace.mark("recognition_started");
 clock = 200; trace.mark("transcript_ready");
 assert.deepEqual(events.map(e => e.elapsedMs), [25,100]);
 assert.equal(events[0].traceId, events[1].traceId);
 assert.deepEqual(Object.keys(events[0]).sort(), ["elapsedMs","stage","traceId"]);
 assert.notEqual(trace.id, new VoiceLatencyTrace(() => {}).id);
});
test("diagnostic sink failure cannot break speech processing", () => {
 const trace = new VoiceLatencyTrace(() => { throw new Error("logger unavailable"); });
 assert.doesNotThrow(() => trace.mark("capture_started"));
});

test("synthesis events wait for audio preparation and preserve sentence identity", async () => {
 const events: VoiceTiming[] = []; let release!: (value: string) => void;
 const trace = new VoiceLatencyTrace(e => events.push(e));
 const pending = prepareSpeech(trace, "sentence-one", () => new Promise<string>(r => release=r));
 assert.deepEqual(events.map(e => e.stage), ["tts_requested"]);
 release("audio"); assert.equal(await pending, "audio");
 assert.deepEqual(events.map(e => e.stage), ["tts_requested","tts_ready"]);
 assert.ok(events.every(e => e.sentenceId === "sentence-one" && e.traceId === trace.id));
});
test("failed synthesis records failure without reporting audio ready", async () => {
 const events: VoiceTiming[] = []; const error = new Error("unavailable");
 await assert.rejects(prepareSpeech(new VoiceLatencyTrace(e => events.push(e)), "sentence-two",
 async () => {throw error;}), e => e === error);
 assert.deepEqual(events.map(e => e.stage), ["tts_requested","tts_failed"]);
});
