import test from "node:test";
import assert from "node:assert/strict";

import { DesktopVoiceGate } from "../src/main/desktop_voice.js";
import { installPermissionPolicy } from "../src/main/permissions.js";
import { PcmSegments, audioBase64, pcmWav } from "../src/renderer/audio/pcm_segments.js";
import {
  MAX_SPEAK_TEXT, MAX_UTTERANCE_BASE64, MAX_UTTERANCE_SAMPLES,
  speakInput, speech, transcribeInput, transcript,
} from "../src/shared/desktop_voice.js";

const SESSION = "0f8fad5b-d9cb-469f-a165-70867728950e";
const loud = () => new Float32Array(1024).fill(0.2);
const quiet = () => new Float32Array(1024);

test("an utterance is bounded to a started session, a language and base64 audio", () => {
  const audio = audioBase64(pcmWav(new Float32Array(16000)));
  assert.deepEqual(transcribeInput({ voice_session_id: SESSION, audio_bytes_b64: audio, language: "pt-BR" }),
    { voice_session_id: SESSION, audio_bytes_b64: audio, audio_format: "wav", language: "pt-BR" });
  for (const bad of [
    { voice_session_id: "not-a-session", audio_bytes_b64: audio, language: "pt-BR" },
    { voice_session_id: SESSION, audio_bytes_b64: "a".repeat(MAX_UTTERANCE_BASE64 + 4), language: "pt-BR" },
    { voice_session_id: SESSION, audio_bytes_b64: "not base64!", language: "pt-BR" },
    { voice_session_id: SESSION, audio_bytes_b64: audio, language: "fr" },
  ]) assert.equal(transcribeInput(bad), undefined);
  // The largest utterance the segmenter emits still fits.
  assert.ok(audioBase64(pcmWav(new Float32Array(MAX_UTTERANCE_SAMPLES))).length <= MAX_UTTERANCE_BASE64);
});

test("a transcript is trimmed text, and a reply to read aloud is bounded", () => {
  assert.deepEqual(transcript({ text: "  olá Ningo " }), { text: "olá Ningo" });
  assert.throws(() => transcript({ text: 3 }));
  assert.deepEqual(speakInput({ text: "Sim.", language: "en" }), { text: "Sim.", language: "en" });
  for (const bad of [{ text: "", language: "en" }, { text: "x".repeat(MAX_SPEAK_TEXT + 1), language: "en" }, { text: "Sim.", language: "es" }]) {
    assert.equal(speakInput(bad), undefined);
  }
  assert.deepEqual(speech({ spoken: "" }), { spoken: "" });
  assert.deepEqual(speech({ spoken: "Sim.", audio_bytes_b64: "UklGRg==" }), { spoken: "Sim.", audio_bytes_b64: "UklGRg==" });
  assert.throws(() => speech({ spoken: "Sim." }));
});

test("the microphone session admits one utterance at a time and ends with stop", async () => {
  const gate = new DesktopVoiceGate();
  assert.equal(gate.listening, false);
  await assert.rejects(gate.turn(SESSION, async () => ({ status: "ok", result: {} })));
  const id = gate.start();
  assert.equal(gate.listening, true);
  await assert.rejects(gate.turn("someone-else", async () => ({ status: "ok", result: {} })));

  let release!: () => void;
  const first = gate.turn(id, () => new Promise((resolve) => { release = () => resolve({ status: "ok", result: {} }); }));
  await assert.rejects(gate.turn(id, async () => ({ status: "ok", result: {} })), "a second utterance waits for the first");
  release();
  assert.deepEqual(await first, { status: "ok", result: {} });

  const pending = gate.turn(id, () => new Promise((resolve) => setTimeout(() => resolve({ status: "ok", result: {} }), 5)));
  gate.stop(id);
  await assert.rejects(pending, /ended/);
  assert.equal(gate.listening, false);
});

test("a reloaded window can start again without being locked out", () => {
  const gate = new DesktopVoiceGate();
  const first = gate.start();
  const second = gate.start();
  assert.notEqual(first, second);
  gate.stop(first);
  assert.equal(gate.listening, true, "stopping a replaced session leaves the new one");
});

test("speech ends after a pause, short blips and leading silence are not sent", () => {
  const segments = new PcmSegments();
  for (let i = 0; i < 20; i++) assert.equal(segments.push(quiet()), undefined, "silence alone is never sent");
  let wav: Uint8Array | undefined;
  for (let i = 0; i < 4; i++) assert.equal(segments.push(loud()), undefined);
  for (let i = 0; i < 12 && !wav; i++) wav = segments.push(quiet());
  assert.ok(wav, "an utterance is sent after about 0.6 s of silence");
  // Leading silence was dropped: at most a fifth of a second of it remains.
  const samples = (wav.length - 44) / 2;
  assert.ok(samples < 4096 + 3200 + 12 * 1024, `utterance holds ${samples} samples`);

  segments.reset();
  assert.equal(segments.push(loud()), undefined);
  let blip: Uint8Array | undefined;
  for (let i = 0; i < 12; i++) blip = segments.push(quiet()) ?? blip;
  assert.equal(blip, undefined, "a sound shorter than 0.16 s is not sent");
});

test("continuous speech is cut at eight seconds", () => {
  const segments = new PcmSegments();
  let wav: Uint8Array | undefined;
  for (let i = 0; i < 200 && !wav; i++) wav = segments.push(loud());
  assert.ok(wav);
  assert.equal((wav.length - 44) / 2, MAX_UTTERANCE_SAMPLES);
});

test("the WAV header says PCM16 mono at 16 kHz, as the voice service expects", () => {
  const wav = pcmWav(new Float32Array([0, 1, -1, 2]));
  const view = new DataView(wav.buffer);
  const text = (at: number, size: number) => String.fromCharCode(...wav.subarray(at, at + size));
  assert.equal(text(0, 4), "RIFF");
  assert.equal(text(8, 8), "WAVEfmt ");
  assert.equal(view.getUint16(20, true), 1, "PCM");
  assert.equal(view.getUint16(22, true), 1, "mono");
  assert.equal(view.getUint32(24, true), 16000);
  assert.equal(view.getUint16(34, true), 16);
  assert.equal(view.getUint32(40, true), 8);
  assert.deepEqual([view.getInt16(46, true), view.getInt16(48, true), view.getInt16(50, true)], [32767, -32767, 32767], "clipped to range");
});

test("the window may open the microphone only while a session listens, and never the camera", () => {
  const page = "file:///app/index.html";
  let listening = false;
  const handlers: { request?: Function; check?: Function } = {};
  installPermissionPolicy({
    setPermissionRequestHandler: (h: Function) => { handlers.request = h; },
    setPermissionCheckHandler: (h: Function) => { handlers.check = h; },
    setDevicePermissionHandler: () => undefined,
  } as never, page, () => listening);
  const contents = { getURL: () => page };
  const ask = (mediaTypes: string[], frame = { isMainFrame: true, requestingUrl: page }) => {
    let granted: boolean | undefined;
    handlers.request!(contents, "media", (answer: boolean) => { granted = answer; }, { ...frame, mediaTypes });
    return granted;
  };
  assert.equal(ask(["audio"]), false, "no session, no microphone");
  listening = true;
  assert.equal(ask(["audio"]), true);
  assert.equal(handlers.check!(contents, "media", "file://", { isMainFrame: true, requestingUrl: page, mediaType: "audio" }), true);
  assert.equal(ask(["video"]), false);
  assert.equal(ask(["audio", "video"]), false);
  assert.equal(ask(["audio"], { isMainFrame: false, requestingUrl: page }), false);
  assert.equal(ask(["audio"], { isMainFrame: true, requestingUrl: "https://example.com/" }), false);
  assert.equal(handlers.check!(contents, "media", "file://", { isMainFrame: true, requestingUrl: page, mediaType: "video" }), false);
});

test("each voice action reads the reply the shell or the orchestrator actually gives", async () => {
  const { ACTIONS } = await import("../src/main/actions.js");
  const gate = new DesktopVoiceGate();
  const started = ACTIONS["voice.desktop.start"].project(gate.startReply().result) as { voice_session_id: string };
  assert.match(started.voice_session_id, /^[a-f0-9-]{36}$/);
  assert.equal(gate.listening, true);
  assert.deepEqual(ACTIONS["voice.desktop.stop"].project(gate.stopReply(started.voice_session_id).result), { stopped: true });
  assert.equal(gate.listening, false);
  // A single-domain request comes back compacted as { domain, result }; communications nests the
  // voice service reply under "voice" (shape seen from a running orchestrator).
  const orchestrator = (domain: string, result: unknown) => ({ event_id: "e", workflow: "w", domain, result });
  assert.deepEqual(ACTIONS["voice.transcribe"].project(orchestrator("communications", { voice: { status: "ok", text: " Olá ", language: "pt-BR", audio_format: "wav" } })), { text: "Olá" });
  // A debug-mode orchestrator answers verbosely, every domain under its own name.
  const verbose = { event: {}, workflow: "voice-audio", domains: ["communications"], result: { communications: { voice: { status: "ok", text: "Oi" } } } };
  assert.deepEqual(ACTIONS["voice.transcribe"].project(verbose), { text: "Oi" });
  assert.deepEqual(ACTIONS["voice.reply.speak"].project(orchestrator("chat", { spoken: "Sim.", audio_bytes_b64: "UklGRg==", audio_format: "wav" })),
    { spoken: "Sim.", audio_bytes_b64: "UklGRg==" });
});

test("a listening microphone stands in for a press only to send a message", async () => {
  const { withVoicePresence } = await import("../src/main/desktop_voice.js");
  const gate = new DesktopVoiceGate();
  const absent = { recent: () => false };
  const presence = withVoicePresence(absent, gate);
  assert.equal(presence.recent("conversation.start"), false, "no session, no start");
  gate.start();
  assert.equal(presence.recent("conversation.start"), true);
  assert.equal(presence.recent("message.created"), true);
  for (const other of ["conversation.delete", "conversation.rename", "voice.desktop.start", undefined]) {
    assert.equal(presence.recent(other), false, `${other} still needs a press`);
  }
  gate.stop();
  assert.equal(presence.recent("conversation.start"), false);
  assert.equal(withVoicePresence({ recent: () => true }, gate).recent("conversation.delete"), true, "a real press still counts");
});
