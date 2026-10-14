export type WhatsAppConfig = {
  // Folder with the linked-device session. Whoever can read it controls the account.
  authDir: string;
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  // Internal port the orchestrator reads the interface's state from. Never published.
  statusPort: number;
  // Answer the account's own "message yourself" chat.
  selfChat: boolean;
  // Phone numbers (digits only) whose direct messages are answered.
  allowedNumbers: string[];
  // Group ids (`<digits>@g.us`) where the bot answers when it is mentioned.
  allowedGroups: string[];
};

type Environment = Record<string, string | undefined>;

export function env(name: string, fallback = "", source: Environment = process.env): string {
  const value = source[name]?.trim();
  return value && value.length > 0 ? value : fallback;
}

const groupPattern = /^[0-9]+(-[0-9]+)*@g\.us$/;

// Nothing is answered unless it is listed: the linked account is usually somebody's own
// number, so there is deliberately no "open" mode for direct messages. Invalid entries stop
// the start-up and name the variable and position, never the value (it is a phone number).
export function buildConfig(source: Environment = process.env): WhatsAppConfig {
  const authDir = env("DOKJA_WHATSAPP_AUTH_DIR", "", source);
  if (!authDir) {
    throw new Error("Set DOKJA_WHATSAPP_AUTH_DIR to the folder that keeps the linked WhatsApp session");
  }

  const config: WhatsAppConfig = {
    authDir,
    orchestratorEndpoint: env("DOKJA_ORCH_HTTP_ENDPOINT", "http://dokja-orchestrator:8091/orchestrator", source),
    orchestratorTimeoutMs: Number(env("DOKJA_ORCHESTRATOR_TIMEOUT_MS", "180000", source)),
    statusPort: parsePort("DOKJA_WHATSAPP_STATUS_PORT", 8094, source),
    selfChat: parseFlag("DOKJA_WHATSAPP_SELF_CHAT", true, source),
    allowedNumbers: parseList("DOKJA_WHATSAPP_ALLOW_FROM", source).map((entry, index) => {
      const number = normalizeNumber(entry);
      if (!number) {
        throw new Error(`DOKJA_WHATSAPP_ALLOW_FROM entry ${index + 1} is not a phone number`);
      }
      return number;
    }),
    allowedGroups: parseList("DOKJA_WHATSAPP_ALLOWED_GROUPS", source).map((entry, index) => {
      if (!groupPattern.test(entry)) {
        throw new Error(`DOKJA_WHATSAPP_ALLOWED_GROUPS entry ${index + 1} is not a group id (<digits>@g.us)`);
      }
      return entry;
    }),
  };
  config.allowedNumbers = unique(config.allowedNumbers);
  config.allowedGroups = unique(config.allowedGroups);

  if (!config.selfChat && config.allowedNumbers.length === 0 && config.allowedGroups.length === 0) {
    throw new Error(
      "Nothing would be answered: enable DOKJA_WHATSAPP_SELF_CHAT or set DOKJA_WHATSAPP_ALLOW_FROM / DOKJA_WHATSAPP_ALLOWED_GROUPS",
    );
  }
  return config;
}

// Keeps the digits of a phone number written in any usual way ("+55 (11) 91234-5678").
// Returns "" when the result is not 7 to 15 digits (the E.164 range).
export function normalizeNumber(value: string): string {
  const digits = value.replace(/[\s().+-]/g, "");
  return /^[0-9]{7,15}$/.test(digits) ? digits : "";
}

// WhatsApp ids for Brazilian mobiles come with or without the ninth digit, depending on when the
// account was created, and people type the number with it. Both spellings name one account.
export function equivalentNumbers(number: string): string[] {
  const mobile = /^55(\d{2})(9)?([6-9]\d{7})$/.exec(number);
  if (!mobile) {
    return [number];
  }
  const [, area, , local] = mobile;
  return [`55${area}9${local}`, `55${area}${local}`];
}

export function numberAllowed(allowed: string[], candidates: string[]): boolean {
  const wanted = new Set(allowed.flatMap(equivalentNumbers));
  return candidates.some((candidate) => equivalentNumbers(candidate).some((number) => wanted.has(number)));
}

function parseList(name: string, source: Environment): string[] {
  return env(name, "", source)
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
}

function parsePort(name: string, fallback: number, source: Environment): number {
  const port = Number(env(name, String(fallback), source));
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`${name} must be a port number`);
  }
  return port;
}

function parseFlag(name: string, fallback: boolean, source: Environment): boolean {
  const value = env(name, "", source).toLowerCase();
  if (!value) {
    return fallback;
  }
  if (value === "true" || value === "1") {
    return true;
  }
  if (value === "false" || value === "0") {
    return false;
  }
  throw new Error(`${name} must be true or false`);
}

function unique<T>(values: T[]): T[] {
  return [...new Set(values)];
}
