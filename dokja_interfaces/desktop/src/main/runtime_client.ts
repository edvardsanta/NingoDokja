import { spawn } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { copyFileSync, existsSync, mkdirSync, realpathSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { createConnection } from "node:net";
import { dirname, isAbsolute, join } from "node:path";
import { isRecord } from "../shared/records.js";
import { OrchestratorError, type OrchestratorReply } from "./orchestrator_client.js";

export function runtimeRoot(start: string): string | undefined {
  let candidate: string;
  try { candidate = realpathSync(start); } catch { return undefined; }
  for (;;) {
    if (existsSync(join(candidate, "docker-compose.lite.yml"))) return candidate;
    const parent = dirname(candidate); if (parent === candidate) return undefined; candidate = parent;
  }
}

function connected(socketPath: string): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = createConnection(socketPath);
    const settle = (answer: boolean) => { socket.destroy(); resolve(answer); };
    socket.once("connect", () => settle(true)); socket.once("error", () => settle(false));
    socket.setTimeout(500, () => settle(false));
  });
}

// Every local interface (desktop, cli, tui) finds the same controller here, one per checkout.
export function runtimeStateHome(env: NodeJS.ProcessEnv, home: string): string {
  const base = env.XDG_STATE_HOME && isAbsolute(env.XDG_STATE_HOME) ? env.XDG_STATE_HOME : join(home, ".local", "state");
  return join(base, "dokja");
}

// legacyStateHome is where earlier desktop builds kept the controller's state; its ownership
// journal is carried over once, so containers it started are still recognized as its own.
export type RuntimeOptions = { root?: string; stateHome: string; legacyStateHome?: string; binary: string; enabled: boolean };

// This client only talks to the bootstrap orchestrator. It never executes a
// container command, accepts a command from the page or reaches a domain service.
export class RuntimeClient {
  readonly stateDir: string;
  readonly socketPath: string;
  readonly projectId: string;
  private starting?: Promise<void>;
  constructor(private readonly options: RuntimeOptions) {
    this.projectId = createHash("sha256").update(options.root ?? "unconfigured").digest("hex").slice(0,16);
    this.stateDir = join(options.stateHome, `runtime-${this.projectId}`);
    this.socketPath = join(this.stateDir, "control.sock");
  }
  private async ensure(): Promise<void> {
    if (!this.options.enabled || !this.options.root) throw new OrchestratorError("unavailable", "Local container controls need a local checkout and the default local orchestrator endpoint.");
    if (await connected(this.socketPath)) return;
    if (!this.starting) {
      this.starting = this.launch().finally(() => { this.starting = undefined; });
    }
    return this.starting;
  }
  private async launch(): Promise<void> {
    if (process.platform === "win32") throw new OrchestratorError("unavailable", "Local runtime control currently requires a Unix host.");
    if (!existsSync(this.options.binary)) throw new OrchestratorError("unavailable", "The runtime controller is missing. Rebuild the desktop application.");
    mkdirSync(this.stateDir, { recursive: true, mode: 0o700 });
    this.adoptLegacyJournal();
    const child = spawn(this.options.binary, ["--root",this.options.root!,"--state-dir",this.stateDir], { detached: true, stdio: "ignore" });
    let failure = false;
    child.once("error", () => { failure = true; }); child.unref();
    for (let attempt=0; attempt<60; attempt++) {
      if (await connected(this.socketPath)) return;
      if (failure) break;
      await new Promise((resolve)=>setTimeout(resolve,50));
    }
    throw new OrchestratorError("unavailable", "The background runtime controller could not start. Check its binary and private state directory permissions.");
  }
  private adoptLegacyJournal(): void {
    if (!this.options.legacyStateHome) return;
    const journal = join(this.stateDir, "ownership.json");
    const legacy = join(this.options.legacyStateHome, `runtime-${this.projectId}`, "ownership.json");
    if (existsSync(journal) || !existsSync(legacy)) return;
    try { copyFileSync(legacy, journal); } catch { /* the controller starts with an empty journal */ }
  }
  async request(type: string, payload: Record<string,unknown>, timeoutMs: number): Promise<OrchestratorReply> {
    if (!["runtime.modules", "runtime.providers", "runtime.providers.configure", "runtime.logs", "runtime.set", "runtime.settings", "runtime.settings.set"].includes(type)) throw new OrchestratorError("denied", "unsupported runtime action");
    await this.ensure();
    const body = JSON.stringify({event_id: randomUUID(), timestamp: new Date().toISOString(), source:"desktop", type,
      user:{id:"desktop-user",name:"desktop"},channel:{id:"desktop"},payload,context:{interface:"desktop"}});
    return new Promise((resolve,reject)=>{
      const req = httpRequest({socketPath:this.socketPath,path:"/request",method:"POST",headers:{"Content-Type":"application/json","Content-Length":Buffer.byteLength(body)}},(res)=>{
        const chunks: Buffer[]=[]; let size=0;
        res.on("data",(chunk:Buffer)=>{size+=chunk.length;if(size>65536){res.destroy(new Error("runtime response too large"));return;}chunks.push(chunk);});
        res.on("error",()=>reject(new OrchestratorError("unavailable","Runtime controller response was interrupted.")));
        res.on("end",()=>{
          try {
            const answer:unknown=JSON.parse(Buffer.concat(chunks).toString());
            if(res.statusCode!==200||!isRecord(answer)||answer.version!==1||answer.project_id!==this.projectId||typeof answer.status!=="string") throw new Error("invalid");
            resolve(answer as OrchestratorReply);
          } catch { reject(new OrchestratorError("unexpected","Runtime controller returned an incompatible response.")); }
        });
      });
      req.setTimeout(Math.min(timeoutMs,15000),()=>req.destroy(new OrchestratorError("timeout","Runtime request timed out; refresh module state before retrying.")));
      req.on("error",(error)=>reject(error instanceof OrchestratorError?error:new OrchestratorError("unavailable","The local runtime controller is unavailable.")));
      req.end(body);
    });
  }
}
