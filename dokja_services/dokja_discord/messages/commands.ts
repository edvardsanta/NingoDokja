import type { DiscordSlashCommand } from "../types.js";

// What the slash commands are called and say, in one place.
//
// A command's base name and description are English and live with the command. Discord shows a
// person the translation for their own client language, and every interaction still carries the
// base names, so handlers read options by their English names whatever the person sees.
// Replies follow the adapter's locale (DOKJA_DISCORD_LOCALE), like the other catalogs here.

type OptionText = { name: string; description: string; choices?: Record<string, string> };
type CommandText = { name: string; description: string; options?: Record<string, OptionText> };

// Translations Discord shows to people whose client is in that language, by base command name.
export const COMMAND_TRANSLATIONS: Record<string, Record<string, CommandText>> = {
  "pt-BR": {
    chat: {
      name: "conversar", description: "Mande uma mensagem para o Ningo.",
      options: { prompt: { name: "mensagem", description: "O que você quer dizer" } },
    },
    meme_fetch: {
      name: "buscar_meme", description: "Busca memes do Ningo.",
      options: { limit: { name: "quantidade", description: "Quantos memes" } },
    },
    meme_refresh: {
      name: "atualizar_memes", description: "Renova o estoque de memes.",
      options: { max_items: { name: "max_itens", description: "Máximo de itens por fonte" } },
    },
    speak: {
      name: "falar", description: "Fala um texto no seu canal de voz.",
      options: { prompt: { name: "texto", description: "O texto a ser falado" } },
    },
    voice_chat_start: {
      name: "conversa_voz", description: "Conversa ao vivo com todos no seu canal de voz.",
      options: {
        mode: {
          name: "modo", description: "Quando o Ningo responde",
          choices: { always: "Sempre", wakeword: "Só depois da palavra de chamada" },
        },
      },
    },
    voice_chat_stop: { name: "parar_conversa_voz", description: "Encerra a conversa de voz neste servidor." },
    play_radio: {
      name: "tocar_radio", description: "Toca uma rádio no seu canal de voz.",
      options: { url: { name: "url", description: "Endereço do stream da rádio" } },
    },
    stop_radio: { name: "parar_radio", description: "Para a rádio neste servidor." },
    ningo_panel: { name: "painel_ningo", description: "Abre o painel de controles do Ningo." },
    conversation_start: {
      name: "iniciar_transcricao", description: "Transcreve o canal de voz; o Ningo fica em silêncio.",
    },
    conversation_stop: {
      name: "parar_transcricao", description: "Encerra o observador e apaga as transcrições temporárias.",
    },
    play: {
      name: "tocar", description: "Toca uma música no seu canal de voz: algumas palavras ou um link.",
      options: { song: { name: "musica", description: "Algumas palavras (música e artista) ou um link" } },
    },
    queue: { name: "fila", description: "Mostra a música tocando e as que estão esperando." },
    history: { name: "historico", description: "Mostra as músicas que você pediu e como terminaram." },
    price: {
      name: "preco", description: "Consulta os preços vigiados: o desconto é de verdade?",
      options: {
        product: { name: "produto", description: "Palavras para buscar, como hd, ssd ou placa de vídeo" },
        brand: { name: "marca", description: "Nome da marca" },
        store: { name: "loja", description: "Só esta loja; as sugestões listam as lojas já vistas" },
      },
    },
    compare_product: {
      name: "comparar_produto", description: "Compara um produto entre as lojas, pelo nome, código ou link.",
      options: {
        product: { name: "produto", description: "Nome ou código do produto, como ssd 1tb" },
        link: { name: "link", description: "Link do produto numa loja; ele é lido, nunca aberto" },
      },
    },
    political_test: {
      name: "teste_politico", description: "O Ningo faz um questionário online e posta o link do resultado.",
      options: { link: { name: "link", description: "Link do questionário" } },
    },
  },
};

// Adds every translation to a command, its options and their choices, in Discord's own shape.
export function translateCommand(command: DiscordSlashCommand): DiscordSlashCommand {
  const nameLocalizations: Record<string, string> = {};
  const descriptionLocalizations: Record<string, string> = {};
  for (const [locale, commands] of Object.entries(COMMAND_TRANSLATIONS)) {
    const text = commands[command.name];
    if (!text) continue;
    nameLocalizations[locale] = text.name;
    descriptionLocalizations[locale] = text.description;
  }
  const options = command.options?.map((option) => {
    const names: Record<string, string> = {};
    const descriptions: Record<string, string> = {};
    const choiceNames = new Map<string, Record<string, string>>();
    for (const [locale, commands] of Object.entries(COMMAND_TRANSLATIONS)) {
      const text = commands[command.name]?.options?.[option.name];
      if (!text) continue;
      names[locale] = text.name;
      descriptions[locale] = text.description;
      for (const [value, label] of Object.entries(text.choices ?? {})) {
        choiceNames.set(value, { ...(choiceNames.get(value) ?? {}), [locale]: label });
      }
    }
    return {
      ...option,
      name_localizations: names,
      description_localizations: descriptions,
      ...(option.choices ? {
        choices: option.choices.map((choice: { name: string; value: string }) => ({
          ...choice, name_localizations: choiceNames.get(String(choice.value)) ?? {},
        })),
      } : {}),
    };
  });
  return {
    ...command,
    name_localizations: nameLocalizations,
    description_localizations: descriptionLocalizations,
    ...(options ? { options } : {}),
  };
}

// The adapter's own words: replies and panel text that no orchestrator reply supplies.
const REPLIES = {
  no_response: { "pt-BR": "Sem resposta.", en: "No response." },
  command_failed: { "pt-BR": "O comando falhou.", en: "Command failed." },
  voice_missing: { "pt-BR": "O controle de voz não está configurado.", en: "Voice controller is not configured." },
  speech_starting: { "pt-BR": "Começando a falar...", en: "Starting speech playback..." },
  speech_failed: { "pt-BR": "Não consegui começar a falar.", en: "Could not start speech playback." },
  voice_starting: { "pt-BR": "Começando a conversa de voz...", en: "Starting voice conversation..." },
  wake_starting: {
    "pt-BR": "Começando a conversa de voz; respondo depois da palavra de chamada...",
    en: "Starting wake-word voice conversation...",
  },
  voice_failed: { "pt-BR": "Não consegui começar a conversa de voz.", en: "Could not start voice conversation." },
  voice_stop_failed: { "pt-BR": "Não consegui encerrar a conversa de voz.", en: "Could not stop voice conversation." },
  radio_starting: { "pt-BR": "Começando a rádio...", en: "Starting radio playback..." },
  radio_failed: { "pt-BR": "Não consegui tocar a rádio.", en: "Could not start the radio." },
  radio_stop_failed: { "pt-BR": "Não consegui parar a rádio.", en: "Could not stop the radio." },
  panel_title: { "pt-BR": "Controles do Ningo", en: "Ningo controls" },
  panel_status: { "pt-BR": "Status do Ningo", en: "Ningo status" },
  panel_meme: { "pt-BR": "Solta um meme aí", en: "Drop a meme" },
  panel_chat: { "pt-BR": "Fale comigo, Ningo!", en: "Talk to me, Ningo!" },
  panel_radio_stop: { "pt-BR": "Parar rádio", en: "Stop radio" },
  panel_meme_count: { "pt-BR": "Escolha quantos memes", en: "Choose how many memes" },
  panel_memes: { "pt-BR": "{count} memes", en: "{count} memes" },
  panel_one_meme: { "pt-BR": "1 meme", en: "1 meme" },
  modal_title: { "pt-BR": "Converse com o Ningo", en: "Chat with Ningo" },
  modal_label: { "pt-BR": "Mensagem", en: "Message" },
  modal_placeholder: { "pt-BR": "Pergunte algo ao Ningo...", en: "Ask Ningo something..." },
} satisfies Record<string, Record<"pt-BR" | "en", string>>;

export type ReplyKey = keyof typeof REPLIES;

// One of the adapter's own texts in a locale, English when the locale has none.
export function adapterText(locale: string | undefined, key: ReplyKey, values: Record<string, string | number> = {}): string {
  const entry: Record<string, string> = REPLIES[key];
  const chosen = locale ?? "pt-BR";
  const text = entry[chosen] ?? entry[chosen.split("-")[0]] ?? entry.en;
  return text.replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? `{${name}}`));
}
