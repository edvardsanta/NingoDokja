// A change (adding a note, a reading) goes through only just after a real click or key press in
// the window. A script in the page can call the shell but cannot fake what the browser reports as
// input, so content that tricks the page into running code cannot write on its own. The window is
// short, so it also cannot wait for an unrelated click.
const WINDOW_MS = 3000;

// What counts as the person being there: a press, not a movement or a release.
const PRESENCE = new Set(["mouseDown", "keyDown", "rawKeyDown", "char", "touchStart"]);

export type Presence = {
  note(inputType: string): void;
  recent(): boolean;
};

export function createPresence(now: () => number = Date.now, windowMs: number = WINDOW_MS): Presence {
  let last = Number.NEGATIVE_INFINITY;
  return {
    note(inputType) {
      if (PRESENCE.has(inputType)) last = now();
    },
    recent: () => now() - last <= windowMs,
  };
}
