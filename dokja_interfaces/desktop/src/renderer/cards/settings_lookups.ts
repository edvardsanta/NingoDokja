import { useCallback, useEffect, useRef, useState } from "react";

import { isRecord } from "../../shared/records.js";
import type { CommunicationChannel, LinkedGroup } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";

// The names behind the ids the settings hold. They come from other parts of the stack and are only
// a courtesy: a lookup that fails leaves the ids as they were, says nothing loudly and is never
// retried by itself.

export type Lookup<T> = {
  // idle: not asked yet; unavailable: asked and not answered.
  phase: "idle" | "loading" | "ready" | "unavailable";
  items: T[];
};

const idle = <T,>(): Lookup<T> => ({ phase: "idle", items: [] });
const DISCORD_ID = /^[0-9]{1,20}$/;

// The Discord channels Messages can see, by name and server, asked once when a list of channels is on
// screen. WhatsApp conversations come in the same answer and are left out.
export function useChannelNames(transport: Transport, wanted: boolean): Lookup<CommunicationChannel> {
  const [state, setState] = useState<Lookup<CommunicationChannel>>(idle);
  const asked = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => {
    if (!wanted || asked.current) return;
    asked.current = true;
    setState({ phase: "loading", items: [] });
    transport.request("communications.channels", {}, { timeoutMs: 15_000 }).then((answer) => {
      if (!mounted.current) return;
      const listed = answer.ok && isRecord(answer.result) && Array.isArray(answer.result.channels) ? answer.result.channels : undefined;
      if (!listed) return setState({ phase: "unavailable", items: [] });
      const items = listed.filter((row): row is CommunicationChannel => isRecord(row) && typeof row.id === "string" && DISCORD_ID.test(row.id) && typeof row.name === "string");
      setState({ phase: "ready", items });
    }, () => { if (mounted.current) setState({ phase: "unavailable", items: [] }); });
  }, [transport, wanted]);
  return state;
}

export type Groups = Lookup<LinkedGroup> & {
  // The last ask was the owner's own, so its failure is worth saying.
  asked: boolean;
  discover: () => Promise<void>;
};

// The groups the linked WhatsApp account is in. Asked quietly once when a list of groups is on screen,
// and again whenever the owner presses the button; only the owner's own ask reports a failure.
export function useGroupNames(transport: Transport, wanted: boolean): Groups {
  const [state, setState] = useState<Lookup<LinkedGroup>>(idle);
  const [asked, setAsked] = useState(false);
  const quiet = useRef(false);
  const latest = useRef(0);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const ask = useCallback(async (own: boolean) => {
    const turn = ++latest.current;
    if (own) setAsked(true);
    setState((previous) => ({ phase: "loading", items: previous.items }));
    const answer = await transport.request("communications.link.groups", { interface: "whatsapp" }).catch(() => undefined);
    if (!mounted.current || turn !== latest.current) return;
    const listed = answer?.ok && isRecord(answer.result) && Array.isArray(answer.result.groups) ? (answer.result.groups as LinkedGroup[]) : undefined;
    setState(listed ? { phase: "ready", items: listed } : { phase: "unavailable", items: [] });
  }, [transport]);
  useEffect(() => {
    if (!wanted || quiet.current) return;
    quiet.current = true;
    void ask(false);
  }, [wanted, ask]);
  const discover = useCallback(() => ask(true), [ask]);
  return { ...state, asked, discover };
}
