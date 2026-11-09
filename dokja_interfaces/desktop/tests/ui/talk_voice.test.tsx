import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AssistantCard } from "../../src/renderer/cards/assistant.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { answering, failure, ok, routedTransport, startedConversation } from "./support.js";

// The browser microphone cannot run here: this stands in for it, recording what was played.
const mic = vi.hoisted(() => ({
  samples: undefined as undefined | ((pcm: Float32Array) => void),
  played: [] as string[],
  stopped: 0,
}));
vi.mock("../../src/renderer/audio/microphone.js", () => ({
  BrowserVoiceAudio: class {
    constructor(samples: (pcm: Float32Array) => void) { mic.samples = samples; }
    async start() {}
    async play(encoded: string) { mic.played.push(encoded); }
    stop() { mic.stopped++; mic.samples = undefined; }
  },
}));

const SESSION = "0f8fad5b-d9cb-469f-a165-70867728950e";
const SPEECH = "UklGRgAAAABXQVZF";

// About a third of a second of speech, then enough silence to end the utterance.
function speak() {
  act(() => {
    for (let i = 0; i < 5; i++) mic.samples?.(new Float32Array(1024).fill(0.2));
    for (let i = 0; i < 12; i++) mic.samples?.(new Float32Array(1024));
  });
}

async function renderTalk(transport: Transport) {
  render(<I18nProvider locale="en"><AssistantCard transport={transport} /></I18nProvider>);
  await waitFor(() => expect(document.querySelector('[role="status"]')?.textContent ?? "").not.toMatch(/Opening the conversation/));
}

afterEach(() => { mic.samples = undefined; mic.played = []; mic.stopped = 0; });

describe("talking by voice in the conversation", () => {
  it("sends what was said as a message of this conversation and reads the reply aloud", async () => {
    const { transport, calls } = routedTransport({
      "voice.desktop.start": ok({ voice_session_id: SESSION }),
      "voice.desktop.stop": ok({ stopped: true }),
      "voice.transcribe": ok({ text: "What did Malthus fear?" }),
      "conversation.start": startedConversation(),
      "message.created": answering("That food would not keep up."),
      "voice.reply.speak": ok({ spoken: "That food would not keep up.", audio_bytes_b64: SPEECH }),
    });
    await renderTalk(transport);

    fireEvent.click(screen.getByRole("button", { name: "Talk by voice" }));
    await screen.findByText("Listening — speak, then pause");
    speak();

    // The spoken message is an ordinary turn: it is kept and shown like a typed one.
    expect(await screen.findByText("What did Malthus fear?")).toBeTruthy();
    expect(await screen.findByText("That food would not keep up.")).toBeTruthy();
    const sent = calls.filter((call) => call.type === "message.created");
    expect(sent).toHaveLength(1);
    expect(sent[0]?.payload?.content).toBe("What did Malthus fear?");
    const transcribed = calls.find((call) => call.type === "voice.transcribe");
    expect(transcribed?.payload).toMatchObject({ voice_session_id: SESSION, language: "en" });
    expect(calls.find((call) => call.type === "voice.reply.speak")?.payload).toEqual({ text: "That food would not keep up.", language: "en" });
    await waitFor(() => expect(mic.played).toEqual([SPEECH]));
    // Listening goes on after the reply.
    expect(await screen.findByText("Listening — speak, then pause")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Stop voice" }));
    expect(mic.stopped).toBe(1);
    expect(calls.at(-1)).toMatchObject({ type: "voice.desktop.stop", payload: { voice_session_id: SESSION } });
  });

  it("sends nothing when nothing was understood", async () => {
    const { transport, calls } = routedTransport({
      "voice.desktop.start": ok({ voice_session_id: SESSION }),
      "voice.transcribe": ok({ text: "  " }),
    });
    await renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Talk by voice" }));
    await screen.findByText("Listening — speak, then pause");
    speak();
    await waitFor(() => expect(calls.some((call) => call.type === "voice.transcribe")).toBe(true));
    expect(await screen.findByText("Listening — speak, then pause")).toBeTruthy();
    expect(calls.some((call) => call.type === "message.created" || call.type === "conversation.start")).toBe(false);
  });

  it("keeps the reply on screen and says so when it cannot be read aloud", async () => {
    const { transport } = routedTransport({
      "voice.desktop.start": ok({ voice_session_id: SESSION }),
      "voice.transcribe": ok({ text: "Hello" }),
      "conversation.start": startedConversation(),
      "message.created": answering("Hello to you."),
      "voice.reply.speak": failure("unavailable", "voice service down"),
    });
    await renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Talk by voice" }));
    await screen.findByText("Listening — speak, then pause");
    speak();
    expect(await screen.findByText("Hello to you.")).toBeTruthy();
    expect((await screen.findByRole("alert")).textContent).toMatch(/could not be read aloud/);
    expect(mic.played).toEqual([]);
    expect(screen.getByRole("button", { name: "Stop voice" })).toBeTruthy();
  });

  it("stops and explains when what was said cannot be transcribed", async () => {
    const { transport, calls } = routedTransport({
      "voice.desktop.start": ok({ voice_session_id: SESSION }),
      "voice.desktop.stop": ok({ stopped: true }),
      "voice.transcribe": failure("unavailable", "voice service down"),
    });
    await renderTalk(transport);
    fireEvent.click(screen.getByRole("button", { name: "Talk by voice" }));
    await screen.findByText("Listening — speak, then pause");
    speak();
    expect((await screen.findByRole("alert")).textContent).toMatch(/could not be sent/);
    expect(screen.getByRole("button", { name: "Talk by voice" })).toBeTruthy();
    expect(calls.some((call) => call.type === "message.created")).toBe(false);
  });
});
