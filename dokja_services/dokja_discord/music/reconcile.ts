// What the orchestrator says a server's queue is, as the bridge hands it back after a music command.
export type QueueState = {
  epoch: string;
  version: number;
  nowPlaying?: { entry: number; url: string; title: string; uploader: string; requestedBy: string; thumbnail: string };
  waiting: number;
  // the track after the one playing, so it can be made ready while this one plays
  next?: { entry: number; url: string };
  paused: boolean;
  volume: number;
};

// What this adapter is playing for a server, by the queue state it last applied.
export type LocalPlayback = {
  epoch?: string;
  version?: number;
  entry?: number;
  paused: boolean;
  volume: number;
};

export type PlaybackAction =
  | { kind: "play"; entry: number; url: string }
  | { kind: "stop" }
  | { kind: "pause" }
  | { kind: "resume" }
  | { kind: "volume"; volume: number };

export const IDLE: LocalPlayback = { paused: false, volume: 100 };

// Reads the bridge's music field; anything malformed is no state at all.
export function parseQueueState(raw: unknown): QueueState | undefined {
  if (!isRecord(raw) || raw.ok !== true) return undefined;
  if (typeof raw.epoch !== "string" || raw.epoch === "" || !Number.isInteger(raw.version)) return undefined;
  const state: QueueState = {
    epoch: raw.epoch,
    version: raw.version as number,
    paused: raw.paused === true,
    volume: clampVolume(raw.volume),
    waiting: Array.isArray(raw.waiting) ? Math.min(raw.waiting.length, 100) : 0,
  };
  const next = Array.isArray(raw.waiting) ? raw.waiting[0] : undefined;
  if (isRecord(next) && Number.isInteger(next.entry) && typeof next.url === "string" && /^https?:\/\//.test(next.url)) {
    state.next = { entry: next.entry as number, url: next.url };
  }
  const now = raw.now_playing;
  if (isRecord(now) && Number.isInteger(now.entry) && typeof now.url === "string" && /^https?:\/\//.test(now.url)) {
    state.nowPlaying = {
      entry: now.entry as number,
      url: now.url,
      title: typeof now.title === "string" ? now.title : "",
      uploader: typeof now.uploader === "string" ? now.uploader : "",
      // a Discord user id, said back as a mention; anything else is left out
      requestedBy: typeof now.requested_by === "string" && /^[0-9]{1,20}$/.test(now.requested_by) ? now.requested_by : "",
      thumbnail: typeof now.thumbnail === "string" && /^https?:\/\//.test(now.thumbnail) && now.thumbnail.length <= 2048 ? now.thumbnail : "",
    };
  }
  return state;
}

// The actions that bring local playback to the queue's state, in order, and the playback they
// leave. A state older than the last one applied in the same epoch (a reply that arrived late) is
// ignored: undefined. A new epoch (the orchestrator restarted) is always newer.
export function reconcile(local: LocalPlayback, state: QueueState): { actions: PlaybackAction[]; next: LocalPlayback } | undefined {
  if (local.epoch === state.epoch && local.version !== undefined && state.version <= local.version) return undefined;
  const actions: PlaybackAction[] = [];
  const sameTrack = local.epoch === state.epoch && local.entry !== undefined && local.entry === state.nowPlaying?.entry;
  if (!state.nowPlaying) {
    if (local.entry !== undefined) actions.push({ kind: "stop" });
  } else if (!sameTrack) {
    actions.push({ kind: "play", entry: state.nowPlaying.entry, url: state.nowPlaying.url });
    if (state.volume !== 100) actions.push({ kind: "volume", volume: state.volume });
    if (state.paused) actions.push({ kind: "pause" });
  } else {
    if (state.volume !== local.volume) actions.push({ kind: "volume", volume: state.volume });
    if (state.paused && !local.paused) actions.push({ kind: "pause" });
    if (!state.paused && local.paused) actions.push({ kind: "resume" });
  }
  return {
    actions,
    next: {
      epoch: state.epoch,
      version: state.version,
      entry: state.nowPlaying?.entry,
      paused: state.nowPlaying ? state.paused : false,
      volume: state.volume,
    },
  };
}

function clampVolume(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? Math.min(100, Math.max(0, Math.round(value))) : 100;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
