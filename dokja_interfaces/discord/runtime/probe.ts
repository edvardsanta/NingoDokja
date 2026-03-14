const DISCORD_API_BASE = "https://discord.com/api/v10";

export type DiscordApplicationProbe = {
  id?: string;
  status?: number;
  error?: string;
  body?: string;
};

function normalizeDiscordToken(raw?: string | null): string | undefined {
  const trimmed = raw?.trim();
  if (!trimmed) {
    return undefined;
  }
  return trimmed.replace(/^Bot\s+/i, "");
}

export async function probeDiscordApplication(
  token: string,
  timeoutMs: number,
  fetcher: typeof fetch = fetch,
): Promise<DiscordApplicationProbe> {
  const normalized = normalizeDiscordToken(token);
  if (!normalized) {
    return { error: "missing token" };
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetcher(`${DISCORD_API_BASE}/oauth2/applications/@me`, {
      headers: { Authorization: `Bot ${normalized}` },
      signal: controller.signal,
    });
    if (!response.ok) {
      const body = await response.text().catch(() => "");
      return {
        status: response.status,
        error: `discord application probe failed (${response.status})`,
        body,
      };
    }
    const payload = (await response.json()) as { id?: string };
    return { id: payload.id };
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : String(error),
    };
  } finally {
    clearTimeout(timeout);
  }
}

export async function fetchDiscordApplicationId(
  token: string,
  timeoutMs: number,
  fetcher: typeof fetch = fetch,
): Promise<string | undefined> {
  const probe = await probeDiscordApplication(token, timeoutMs, fetcher);
  return probe.id;
}
