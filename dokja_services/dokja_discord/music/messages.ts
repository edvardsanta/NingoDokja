// What music says to people, in the adapter's locale (DOKJA_DISCORD_LOCALE). The orchestrator
// sends the queue and refusal codes; the words are the interface's, as for chat notices.
const MESSAGES = {
  looking: { "pt-BR": "Procurando...", en: "Looking for it..." },
  moment: { "pt-BR": "Um momento...", en: "One moment..." },
  onlyServer: { "pt-BR": "Música só toca no canal de voz de um servidor.", en: "Music plays only in a server's voice channel." },
  noVoice: { "pt-BR": "O controle de voz não está configurado.", en: "Voice controller is not configured." },
  joinVoice: { "pt-BR": "Entre em um canal de voz para tocar música.", en: "Join a voice channel to play music." },
  playingElsewhere: { "pt-BR": "A música está tocando em <#{channel}>. Entre lá para controlar a fila.", en: "Music is playing in <#{channel}>. Join it to control the queue." },
  couldNotPlay: { "pt-BR": "Não consegui tocar.", en: "Could not play it." },
  failed: { "pt-BR": "Não deu para fazer isso.", en: "That could not be done." },
  done: { "pt-BR": "Feito.", en: "Done." },
  nowPlaying: { "pt-BR": "Tocando agora: {song}.", en: "Now playing: {song}." },
  queued: { "pt-BR": "Na fila (#{place}): {song}.", en: "Queued #{place}: {song}." },
  skippedEmpty: { "pt-BR": "Pulei. A fila acabou.", en: "Skipped. The queue is empty." },
  skippedNext: { "pt-BR": "Pulei. Tocando agora: {song}.", en: "Skipped. Now playing: {song}." },
  paused: { "pt-BR": "Pausado.", en: "Paused." },
  resumed: { "pt-BR": "Retomado.", en: "Resumed." },
  stopped: { "pt-BR": "Parei. A fila está vazia.", en: "Stopped. The queue is empty." },
  volume: { "pt-BR": "Volume: {volume}%.", en: "Volume: {volume}%." },
  nothingPlaying: { "pt-BR": "Nada tocando.", en: "Nothing is playing." },
  queueNow: { "pt-BR": "Tocando agora: {song}", en: "Now playing: {song}" },
  untitled: { "pt-BR": "uma faixa sem título", en: "an untitled track" },
  // refusals, by the orchestrator's code
  no_server: { "pt-BR": "Música só toca no canal de voz de um servidor.", en: "Music plays only in a server's voice channel." },
  empty_request: { "pt-BR": "Diga o que tocar: algumas palavras ou um link.", en: "Say what to play: a few words or a link." },
  queue_full: { "pt-BR": "A fila está cheia: espere uma música acabar.", en: "The queue is full: wait for a song to end." },
  no_source: { "pt-BR": "Nenhuma fonte de música está configurada.", en: "No music source is set up." },
  not_found: { "pt-BR": "Não achei essa música.", en: "I could not find that song." },
  nothing_playing: { "pt-BR": "Nada está tocando.", en: "Nothing is playing." },
  bad_volume: { "pt-BR": "O volume vai de 0 a 100.", en: "The volume goes from 0 to 100." },
  not_set_up: { "pt-BR": "A música não está configurada.", en: "Music is not set up." },
  track_refused: { "pt-BR": "Não dá para tocar isso: {reason}", en: "That cannot be played: {reason}" },
  // the panel
  panelPlaying: { "pt-BR": "🎶 Tocando agora", en: "🎶 Now playing" },
  panelPaused: { "pt-BR": "⏸ Pausado", en: "⏸ Paused" },
  askedBy: { "pt-BR": "pedido por <@{user}>", en: "asked by <@{user}>" },
  waiting: { "pt-BR": "{count} na fila", en: "{count} waiting" },
  nothingWaiting: { "pt-BR": "nada na fila", en: "nothing waiting" },
  queueOver: { "pt-BR": "⏹ A fila acabou. `/play` começa outra.", en: "⏹ The queue is over. `/play` starts a new one." },
  pause: { "pt-BR": "⏸ Pausar", en: "⏸ Pause" },
  resume: { "pt-BR": "▶ Retomar", en: "▶ Resume" },
  skip: { "pt-BR": "⏭ Pular", en: "⏭ Skip" },
  stop: { "pt-BR": "⏹ Parar", en: "⏹ Stop" },
} as const;

export type MessageKey = keyof typeof MESSAGES;

export function say(locale: string, key: MessageKey, vars: Record<string, string | number> = {}): string {
  const entry = MESSAGES[key] as Record<string, string>;
  const text = entry[locale] ?? entry[locale.split("-")[0]] ?? entry.en;
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in vars ? String(vars[name]) : whole));
}

type Track = { title?: unknown; uploader?: unknown };

export function songOf(locale: string, track: Track | undefined): string {
  const title = typeof track?.title === "string" && track.title.trim() ? track.title.trim() : say(locale, "untitled");
  const uploader = typeof track?.uploader === "string" ? track.uploader.trim() : "";
  return uploader ? `${title} – ${uploader}` : title;
}

// The reply to a music command, from what the orchestrator answered: the queue, or a refusal code.
// Without a music answer (an older orchestrator), its own text stands.
export function musicReplyText(locale: string, eventType: string, music: unknown, fallback: string): string {
  if (!isRecord(music)) return fallback || say(locale, "done");
  if (music.ok !== true) {
    const code = typeof music.code === "string" ? music.code : "";
    const reason = typeof music.reason === "string" ? music.reason : "";
    if (code === "track_refused") return say(locale, "track_refused", { reason });
    if (code in MESSAGES) return say(locale, code as MessageKey);
    return reason || fallback || say(locale, "failed");
  }
  const now = isRecord(music.now_playing) ? (music.now_playing as Track) : undefined;
  switch (eventType) {
    case "music.play": {
      const added = isRecord(music.added) ? (music.added as Track) : undefined;
      const place = typeof music.place === "number" ? music.place : 0;
      return place > 0 ? say(locale, "queued", { place, song: songOf(locale, added) }) : say(locale, "nowPlaying", { song: songOf(locale, added) });
    }
    case "music.skip":
      return now ? say(locale, "skippedNext", { song: songOf(locale, now) }) : say(locale, "skippedEmpty");
    case "music.pause":
      return say(locale, "paused");
    case "music.resume":
      return say(locale, "resumed");
    case "music.stop":
      return say(locale, "stopped");
    case "music.volume":
      return say(locale, "volume", { volume: typeof music.volume === "number" ? music.volume : 0 });
  }
  if (!now) return say(locale, "nothingPlaying");
  const waiting = Array.isArray(music.waiting) ? (music.waiting as Track[]) : [];
  return [say(locale, "queueNow", { song: songOf(locale, now) }), ...waiting.map((track, index) => `${index + 1}. ${songOf(locale, track)}`)].join("\n");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
