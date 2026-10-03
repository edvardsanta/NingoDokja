import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { Reply } from "zeromq";

const ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

// The app needs a display. A person's own screen is the wrong place for it: the window takes the
// focus, and whatever they type elsewhere lands in it as the real input the tests must be able to
// rule out. So the app is shown in a Wayland compositor with no screen (weston's headless backend)
// when there is one, and on the person's display only when there is not.
const WESTON = ["/usr/bin/weston", "/usr/local/bin/weston"].find((path) => existsSync(path));
const RUNTIME_DIR = process.env.XDG_RUNTIME_DIR ?? "";

export const hasDisplay = Boolean((WESTON && RUNTIME_DIR) || process.env.WAYLAND_DISPLAY || process.env.DISPLAY);

export const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

// What the fake orchestrator says. Hand-built, never a captured reply, and seeded with what must
// not reach the screen: a channel ID, a provider profile, a plugin name and an item address.
export const COMMUNICATIONS_TOKEN = "test-communications-token-0000000000";
export const HOSTILE_TITLE = '<img src="x" onerror="window.__owned = 1">';

function replyFor(type: string): Record<string, unknown> {
  const wrap = (domain: string, body: Record<string, unknown>) => ({
    status: "ok",
    result: { event_id: "event-1", workflow: domain, domain, result: body },
  });
  switch (type) {
    case "communications.channels":
      return wrap("communications", { channels: [{ id: "123", name: "reading-room" }] });
    case "communications.history":
      return wrap("communications", { channel_id: "123", before: "", messages: [
        { id: "100", author: "A reader", bot: false, content: HOSTILE_TITLE, timestamp: "2026-10-03T00:00:00Z", attachments: ["clip.mp4"], token: "private-field" },
        { id: "101", author: "Ningo", bot: true, content: "Every reading deserves a question.", timestamp: "2026-10-03T00:01:00Z", attachments: [] },
      ] });
    case "ningo.status":
      return wrap("system", {
        status: "ok",
        services: { meme: { status: "ok", enabled: true }, feeds: { status: "ok", detail: "no plugins enabled" } },
        jobs: [{ name: "meme.refresh", enabled: true, interval: "6h0m0s", interval_override: false, next_at: "2026-10-03T12:00:00Z", last_at: "2026-10-03T06:00:00Z", last_outcome: "ran", last_error: "", announced_at: "announced-1" }],
        channels: { meme: ["channel-1"] },
        chat_profiles: { profiles: [{ name: "profile-1", base_url: "base-url-1", key_hint: "hint-1" }] },
      });
    case "services.set":
      return wrap("system", { name: "meme", enabled: false });
    case "scheduler.jobs.set":
      return wrap("system", { name: "meme.refresh", enabled: false, interval: "6h0m0s", interval_override: false, secret: "private-field" });
    case "knowledge.ingest":
      return wrap("knowledge", {
        source_id: "note:a-thought-0123456789",
        created: true, changed: true, chunks: 2, embedded: 0, degraded: true, reason: "no embedding server is configured",
      });
    case "knowledge.list":
      return wrap("knowledge", {
        documents: [
          { source_id: "note:a-1", title: HOSTILE_TITLE, kind: "note", source_ref: "Book, p. 12", tags: ["a"], chunks: 2, embedded: 2, updated_at: "2026-10-02T10:00:00", content_sha: "hash-1", path: "/private/path-1" },
        ],
        total: 1, offset: 0,
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
  // the type, the source and the payload of every event it received, in order
  seen: Array<{ type: string; source: string; payload: Record<string, unknown> }>;
  stop(): Promise<void>;
};

export async function startFakeOrchestrator(): Promise<FakeOrchestrator> {
  const socket = new Reply();
  await socket.bind("tcp://127.0.0.1:*");
  const seen: FakeOrchestrator["seen"] = [];
  const loop = (async () => {
    for await (const [frame] of socket) {
      const event = JSON.parse(frame?.toString() ?? "{}") as { type?: string; source?: string; payload?: Record<string, unknown>; context?: Record<string, unknown> };
      seen.push({ type: String(event.type), source: String(event.source), payload: event.payload ?? {} });
      const answer = String(event.type).startsWith("communications.") && event.context?.communications_token !== COMMUNICATIONS_TOKEN
        ? { status: "error", error: "communications access denied" } : replyFor(String(event.type));
      await socket.send(JSON.stringify(answer));
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

export type RunningApp = { cdpPort: number; isolated: boolean; stop(): Promise<void> };

// A compositor with no screen, on a socket of its own. Undefined when weston is not installed.
async function startHiddenDisplay(): Promise<{ socket: string; stop(): Promise<void> } | undefined> {
  if (!WESTON || !RUNTIME_DIR) return undefined;
  const socket = `dokja-e2e-${process.pid}-${Date.now()}`;
  const child = spawn(
    WESTON,
    ["--backend=headless", "--renderer=pixman", `--socket=${socket}`, "--idle-time=0", "--width=1280", "--height=800"],
    { stdio: "ignore", detached: true },
  );
  const stop = async () => {
    if (child.exitCode === null) {
      child.kill("SIGTERM");
      await Promise.race([new Promise((resolve) => child.once("exit", resolve)), sleep(3000)]);
    }
    await rm(join(RUNTIME_DIR, socket), { force: true });
    await rm(join(RUNTIME_DIR, `${socket}.lock`), { force: true });
  };
  for (let waited = 0; waited < 10_000; waited += 100) {
    if (existsSync(join(RUNTIME_DIR, socket))) return { socket, stop };
    if (child.exitCode !== null) break;
    await sleep(100);
  }
  await stop();
  return undefined;
}

// Starts the built app (run `pnpm build` first) with its own profile, against the fake
// orchestrator, and waits for the debugging port Chromium reports.
export async function launchApp(endpoint: string, communicationsToken = ""): Promise<RunningApp> {
  const profile = await mkdtemp(join(tmpdir(), "dokja-desktop-e2e-"));
  const hidden = await startHiddenDisplay();
  const env = hidden
    ? { ...process.env, WAYLAND_DISPLAY: hidden.socket, DISPLAY: "" }
    : process.env;
  const child: ChildProcess = spawn(
    join(ROOT, "node_modules", ".bin", "electron"),
    [
      ".",
      ...(hidden ? ["--ozone-platform=wayland", "--disable-gpu"] : []),
      "--remote-debugging-port=0",
      `--user-data-dir=${profile}`,
      `--request-endpoint=${endpoint}`,
      "--lang=en",
    ],
    { cwd: ROOT, env: { ...env, DOKJA_COMMUNICATIONS_TOKEN: communicationsToken }, detached: true, stdio: ["ignore", "ignore", "ignore"] },
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
    await hidden?.stop();
    await rm(profile, { recursive: true, force: true });
  };

  try {
    // Chromium writes the port it chose to <profile>/DevToolsActivePort, first line.
    for (let waited = 0; waited < 40_000; waited += 200) {
      if (child.exitCode !== null) throw new Error(`the app exited with code ${child.exitCode}`);
      const port = Number((await readFile(join(profile, "DevToolsActivePort"), "utf8").catch(() => "")).split("\n")[0]);
      if (port > 0) return { cdpPort: port, isolated: hidden !== undefined, stop };
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

  private async send(method: string, params: Record<string, unknown>) {
    const id = ++this.next;
    const answer = new Promise<{ result?: { result?: { value?: unknown }; exceptionDetails?: unknown }; error?: unknown }>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiting.delete(id);
        reject(new Error(`debugging command timed out: ${method}`));
      }, 10000);
      this.waiting.set(id, (result) => {
        clearTimeout(timer);
        this.waiting.delete(id);
        resolve(result);
      });
    });
    this.socket.send(JSON.stringify({ id, method, params }));
    return answer;
  }

  async evaluate<T>(expression: string): Promise<T> {
    const message = await this.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
    if (message.error || message.result?.exceptionDetails) {
      throw new Error(`evaluate failed: ${JSON.stringify(message.error ?? message.result?.exceptionDetails)}`);
    }
    return message.result?.result?.value as T;
  }

  // A press of the mouse as the browser itself reports it, unlike element.click() from a script.
  async click(x: number, y: number): Promise<void> {
    for (const type of ["mouseMoved", "mousePressed", "mouseReleased"]) {
      await this.send("Input.dispatchMouseEvent", { type, x, y, button: "left", clickCount: 1 });
    }
  }

  // A key press as the browser itself reports it.
  async press(key: string): Promise<void> {
    await this.send("Input.dispatchKeyEvent", { type: "keyDown", key, text: key });
    await this.send("Input.dispatchKeyEvent", { type: "keyUp", key });
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
