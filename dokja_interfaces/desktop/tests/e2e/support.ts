import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { Reply } from "zeromq";

const ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

export const hasDisplay = Boolean(process.env.WAYLAND_DISPLAY || process.env.DISPLAY);

export const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

// What the fake orchestrator says. Hand-built, never a captured reply, and seeded with what must
// not reach the screen: a channel ID, a provider profile, a plugin name and an item address.
export const HOSTILE_TITLE = '<img src="x" onerror="window.__owned = 1">';

function replyFor(type: string): Record<string, unknown> {
  const wrap = (domain: string, body: Record<string, unknown>) => ({
    status: "ok",
    result: { event_id: "event-1", workflow: domain, domain, result: body },
  });
  switch (type) {
    case "ningo.status":
      return wrap("system", {
        status: "ok",
        services: { meme: { status: "ok", enabled: true }, feeds: { status: "ok", detail: "no plugins enabled" } },
        channels: { meme: ["channel-1"] },
        chat_profiles: { profiles: [{ name: "profile-1", base_url: "base-url-1", key_hint: "hint-1" }] },
      });
    case "digest.status":
      return wrap("digest", {
        configured: true,
        directory_error: "",
        ok: 1, failed: 0, pending: 0, disabled: 0, invalid: 0, items: 2,
        plugins: [{ id: "source-1", name: "First source", state: "ok", items: 2, last_ok: "2026-10-02T10:00:00Z", last_error: "" }],
      });
    case "digest.items":
      return wrap("digest", {
        items: [
          { id: "1", plugin: "source-1", title: HOSTILE_TITLE, summary: "A summary", url: "https://example.com/entry-1", published: "2026-10-02T09:00:00Z", source: "First source" },
          { id: "2", plugin: "source-1", title: "A plain title", summary: "", url: "", published: "", source: "First source" },
        ],
        total: 2, offset: 0, more: 0, updated: "2026-10-02T10:00:00Z",
      });
  }
  return { status: "error", error: `unexpected event ${type}` };
}

export type FakeOrchestrator = {
  endpoint: string;
  // the type and the source of every event it received, in order
  seen: Array<{ type: string; source: string }>;
  stop(): Promise<void>;
};

export async function startFakeOrchestrator(): Promise<FakeOrchestrator> {
  const socket = new Reply();
  await socket.bind("tcp://127.0.0.1:*");
  const seen: FakeOrchestrator["seen"] = [];
  const loop = (async () => {
    for await (const [frame] of socket) {
      const event = JSON.parse(frame?.toString() ?? "{}") as { type?: string; source?: string };
      seen.push({ type: String(event.type), source: String(event.source) });
      await socket.send(JSON.stringify(replyFor(String(event.type))));
    }
  })().catch(() => undefined);
  return {
    endpoint: socket.lastEndpoint ?? "",
    seen,
    async stop() {
      socket.close();
      await loop;
    },
  };
}

// A web server that must never be reached: anything the app loads from it counts as a hit.
export type Tripwire = { origin: string; hits: string[]; stop(): Promise<void> };

export async function startTripwire(): Promise<Tripwire> {
  const hits: string[] = [];
  const server: Server = createServer((request, response) => {
    hits.push(request.url ?? "");
    response.writeHead(200, { "access-control-allow-origin": "*", "content-type": "image/png" });
    response.end();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    origin: `http://127.0.0.1:${port}`,
    hits,
    stop: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

export type RunningApp = { cdpPort: number; stop(): Promise<void> };

// Starts the built app (run `pnpm build` first) with its own profile, against the fake
// orchestrator, and waits for the debugging port Chromium reports.
export async function launchApp(endpoint: string): Promise<RunningApp> {
  const profile = await mkdtemp(join(tmpdir(), "dokja-desktop-e2e-"));
  const child: ChildProcess = spawn(
    join(ROOT, "node_modules", ".bin", "electron"),
    [".", "--remote-debugging-port=0", `--user-data-dir=${profile}`, `--request-endpoint=${endpoint}`, "--lang=en"],
    { cwd: ROOT, detached: true, stdio: ["ignore", "ignore", "ignore"] },
  );

  const stop = async () => {
    for (const signal of ["SIGTERM", "SIGKILL"] as const) {
      if (child.pid === undefined || child.exitCode !== null) break;
      try {
        process.kill(-child.pid, signal);
      } catch {
        break;
      }
      await Promise.race([new Promise((resolve) => child.once("exit", resolve)), sleep(4000)]);
    }
    await rm(profile, { recursive: true, force: true });
  };

  try {
    // Chromium writes the port it chose to <profile>/DevToolsActivePort, first line.
    for (let waited = 0; waited < 40_000; waited += 200) {
      if (child.exitCode !== null) throw new Error(`the app exited with code ${child.exitCode}`);
      const port = Number((await readFile(join(profile, "DevToolsActivePort"), "utf8").catch(() => "")).split("\n")[0]);
      if (port > 0) return { cdpPort: port, stop };
      await sleep(200);
    }
    throw new Error("the app did not open its debugging port");
  } catch (error) {
    await stop();
    throw error;
  }
}

// The Chrome DevTools Protocol, only as much as these tests need: evaluate an expression in the page.
export class Page {
  private next = 0;
  private readonly waiting = new Map<number, (message: { result?: { result?: { value?: unknown }; exceptionDetails?: unknown }; error?: unknown }) => void>();

  private constructor(private readonly socket: WebSocket) {
    socket.onmessage = (message) => {
      const data = JSON.parse(String(message.data)) as { id?: number };
      if (data.id !== undefined) this.waiting.get(data.id)?.(data as never);
    };
  }

  static async connect(port: number): Promise<Page> {
    for (let waited = 0; waited < 30_000; waited += 250) {
      const targets = (await fetch(`http://127.0.0.1:${port}/json`).then((response) => response.json()).catch(() => [])) as Array<{
        type: string;
        url: string;
        webSocketDebuggerUrl: string;
      }>;
      const target = targets.find((candidate) => candidate.type === "page" && candidate.url.startsWith("file:"));
      if (target) {
        const socket = new WebSocket(target.webSocketDebuggerUrl);
        await new Promise<void>((resolve, reject) => {
          socket.onopen = () => resolve();
          socket.onerror = () => reject(new Error("could not open the debugging socket"));
        });
        return new Page(socket);
      }
      await sleep(250);
    }
    throw new Error("the app page never appeared");
  }

  async evaluate<T>(expression: string): Promise<T> {
    const id = ++this.next;
    const answer = new Promise<{ result?: { result?: { value?: unknown }; exceptionDetails?: unknown }; error?: unknown }>((resolve) =>
      this.waiting.set(id, resolve),
    );
    this.socket.send(
      JSON.stringify({ id, method: "Runtime.evaluate", params: { expression, returnByValue: true, awaitPromise: true } }),
    );
    const message = await answer;
    if (message.error || message.result?.exceptionDetails) {
      throw new Error(`evaluate failed: ${JSON.stringify(message.error ?? message.result?.exceptionDetails)}`);
    }
    return message.result?.result?.value as T;
  }

  // Polls until the expression is truthy, then returns its value.
  async waitFor<T>(expression: string, limitMs = 15_000): Promise<T> {
    for (let waited = 0; waited < limitMs; waited += 150) {
      const value = await this.evaluate<T>(expression).catch(() => undefined);
      if (value) return value;
      await sleep(150);
    }
    throw new Error(`timed out waiting for: ${expression}`);
  }

  close() {
    this.socket.close();
  }
}
