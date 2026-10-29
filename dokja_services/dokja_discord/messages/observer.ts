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
  onlyParticipant: {
    "pt-BR": "Somente um participante que consentiu pode encerrar este observador",
    en: "Only a consenting participant can stop this observer",
  },
  stopped: {
    "pt-BR": "Observador de conversa encerrado; transcrições temporárias excluídas.",
    en: "Conversation observer stopped; ephemeral transcripts deleted.",
  },
  inactive: {
    "pt-BR": "Nenhum observador está ativo",
    en: "No observer is active",
  },
  left: {
    "pt-BR": "Seu consentimento para transcrição foi retirado e suas transcrições temporárias foram excluídas.",
    en: "Your transcription consent was withdrawn and your ephemeral transcripts deleted.",
  },
  alreadyActive: {
    "pt-BR": "Já existe um observador; use /conversation_join para consentir",
    en: "An observer already exists; use /conversation_join to consent",
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
    "pt-BR": "Observador de transcrição ativo. Somente participantes que consentiram explicitamente são capturados. Use /conversation_join para participar, /conversation_leave para retirar o consentimento ou /conversation_stop para encerrar e excluir as transcrições. Abra Realtime no Dokja Desktop para ver as transcrições finalizadas de cada segmento. Nenhum áudio bruto é armazenado; as transcrições expiram após 15 minutos de inatividade. Ningo não falará neste modo.",
    en: "Transcription observer active. Only explicitly consenting participants are captured. Use /conversation_join to opt in, /conversation_leave to withdraw, or /conversation_stop to end and delete. Open Realtime in Dokja Desktop to view segment-final transcripts. No raw audio is retained; transcripts expire after 15 minutes of inactivity. Ningo will not speak in this mode.",
  },
  joinChannel: {
    "pt-BR": "Entre no canal de voz do observador e use o canal de texto original",
    en: "Join the observer voice channel and use the original text channel",
  },
  newSession: {
    "pt-BR": "Inicie uma nova sessão do observador antes de consentir novamente; o áudio na fila desta sessão foi invalidado",
    en: "Start a new observer session before consenting again; queued audio from this session is invalidated",
  },
  ended: {
    "pt-BR": "O observador foi encerrado durante sua entrada",
    en: "Observer ended while joining",
  },
  joined: {
    "pt-BR": "Você consentiu com a transcrição temporária nesta sessão do observador. Use /conversation_leave para retirar o consentimento.",
    en: "You consented to ephemeral transcription in this observer session. Use /conversation_leave to withdraw.",
  },
  conversation_start: {
    "pt-BR": "Inicie um observador para quem consentir; Ningo fica em silêncio.",
    en: "Start an observer for consenting speakers; Ningo stays silent.",
  },
  conversation_join: {
    "pt-BR": "Consinta com a transcrição na sessão ativa do observador.",
    en: "Consent to transcription in the active observer session.",
  },
  conversation_leave: {
    "pt-BR": "Retire o consentimento e exclua suas transcrições temporárias.",
    en: "Withdraw consent and delete your ephemeral transcripts.",
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
