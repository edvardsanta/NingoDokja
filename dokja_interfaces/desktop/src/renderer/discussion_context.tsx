import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

import { useOpenWorkspace } from "./sidebar.js";

// A reading the owner chose to discuss: the screen names it by id, as the orchestrator does. Its
// prompts are the questions its own source suggests, written from its text.
export type Reading = { id: string; title: string; source: string; prompts?: readonly string[] };

type Discussion = {
  reading: Reading | undefined;
  // Each choice is a new conversation, even of the same reading twice.
  round: number;
  choose: (reading: Reading | undefined) => void;
};

const DiscussionContext = createContext<Discussion>({ reading: undefined, round: 0, choose: () => {} });

// What Talk is about, kept above the workspaces so a press in Readings can hand Talk a reading.
// There is one composer, in Talk; a reading is discussed there, never in the list.
export function DiscussionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{ reading: Reading | undefined; round: number }>({ reading: undefined, round: 0 });
  const choose = useCallback((reading: Reading | undefined) => setState((current) => ({ reading, round: current.round + 1 })), []);
  return <DiscussionContext.Provider value={{ ...state, choose }}>{children}</DiscussionContext.Provider>;
}

export function useDiscussion(): Discussion {
  return useContext(DiscussionContext);
}

// Opens Talk on a new conversation about the reading.
export function useDiscuss(): (reading: Reading) => void {
  const { choose } = useDiscussion();
  const open = useOpenWorkspace();
  return useCallback((reading: Reading) => {
    choose(reading);
    open("assistant");
  }, [choose, open]);
}
