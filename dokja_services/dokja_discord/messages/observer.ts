// Observer interface text follows DOKJA_DISCORD_LOCALE, like music messages.
const MESSAGES = {
  notAccepted: {
    "pt-BR": "A solicitação do observador não foi autorizada ou aceita",
    en: "Observer request was not authorized or accepted",
  },
  requiresVoice: {
    "pt-BR": "O observador precisa de um controle de voz em um servidor",
    en: "Observer requires a guild voice controller",
  },
  stopped: {
    "pt-BR": "Observador de conversa encerrado; transcrições temporárias excluídas.",
    en: "Conversation observer stopped; ephemeral transcripts deleted.",
  },
  inactive: {
    "pt-BR": "Nenhum observador está ativo",
    en: "No observer is active",
  },
  alreadyActive: {
    "pt-BR": "Já existe um observador neste servidor",
    en: "An observer already exists in this server",
  },
  disconnected: {
    "pt-BR": "Observador de conversa encerrado. A sessão não aceita mais áudio; a exclusão do registro temporário foi solicitada.",
    en: "Conversation observer stopped. The session no longer accepts audio; ephemeral journal deletion was requested.",
  },
  deliveryFailed: {
    "pt-BR": "O observador foi encerrado porque houve uma falha no envio das transcrições. A captura de áudio está desativada; a exclusão foi solicitada e os dados temporários restantes expiram após inatividade.",
    en: "Observer stopped because transcript delivery failed. Audio capture is disabled; deletion was requested and remaining ephemeral data expires after inactivity.",
  },
  active: {
    "pt-BR": "Transcrição iniciada.",
    en: "Transcription started.",
  },
  joinChannel: {
    "pt-BR": "Entre no canal de voz do observador e use o canal de texto original",
    en: "Join the observer voice channel and use the original text channel",
  },
  conversation_start: {
    "pt-BR": "Inicie a transcrição do canal de voz; Ningo fica em silêncio.",
    en: "Start transcribing the voice channel; Ningo stays silent.",
  },
  conversation_stop: {
    "pt-BR": "Encerre o observador e exclua suas transcrições temporárias.",
    en: "Stop the observer and delete its ephemeral transcripts.",
  },
  busy: {
    "pt-BR": "Uma operação do observador já está em andamento",
    en: "An observer operation is already in progress",
  },
  failed: {
    "pt-BR": "Falha no observador",
    en: "Observer failed",
  },
} as const;

export function observerMessage(locale: string, key: keyof typeof MESSAGES): string {
  const entry: Readonly<Record<string, string>> = MESSAGES[key];
  return entry[locale] ?? entry[locale.split("-")[0]] ?? entry.en;
}
