import { useEffect, useRef, useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { LiveDesktopVoice, type Converse, type VoiceIssue, type VoiceState } from "../audio/live_voice.js";
import type { VoiceAudioFactory } from "../audio/microphone.js";
import { useTranslate } from "../i18n/context.js";
import { Button } from "./ui.js";

// Literal ids, so the catalog check can see every state message.
const STATE_MESSAGE = {
  idle: "voice_state_idle",
  preparing: "voice_state_preparing",
  listening: "voice_state_listening",
  thinking: "voice_state_thinking",
  speaking: "voice_state_speaking",
} as const satisfies Record<VoiceState, string>;

// Talk by voice, beside the composer. What is said is sent as an ordinary message of this
// conversation, so it is kept, focused and retried like a typed one; the reply is read aloud.
export function TalkVoice({ transport, converse, disabled = false, audioFactory }: { transport: Transport; converse: Converse; disabled?: boolean; audioFactory?: VoiceAudioFactory }) {
  const t = useTranslate();
  const [state, setState] = useState<VoiceState>("idle");
  const [issue, setIssue] = useState<VoiceIssue>();
  // The latest send function, so a message is always sent to the conversation on screen.
  const send = useRef(converse);
  send.current = converse;
  const controller = useRef<LiveDesktopVoice | undefined>(undefined);
  useEffect(() => {
    const voice = new LiveDesktopVoice(transport, { state: setState, issue: setIssue }, (text) => send.current(text), audioFactory);
    controller.current = voice;
    return () => { voice.stop(); if (controller.current === voice) controller.current = undefined; };
  }, [transport, audioFactory]);
  const listening = state !== "idle";
  const toggle = () => {
    if (listening) { controller.current?.stop(); return; }
    setIssue(undefined);
    void controller.current?.start(t("voice_language") === "en" ? "en" : "pt-BR");
  };
  return <div className="talk-voice">
    <Button size="compact" aria-pressed={listening} disabled={disabled && !listening} onClick={toggle}>{t(listening ? "voice_stop" : "voice_start")}</Button>
    {listening && <p className="detail talk-voice-state" aria-live="polite" aria-atomic="true">{t(STATE_MESSAGE[state])}</p>}
    {issue && <p className="talk-voice-issue" data-tone="problem" role="alert">{t(issue)}</p>}
  </div>;
}
