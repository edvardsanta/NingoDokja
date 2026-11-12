import assert from "node:assert/strict";
import { test } from "node:test";
import { createServer } from "node:http";
import { connect } from "node:net";
import { once } from "node:events";
import { spawn } from "node:child_process";
import { createShutdown, closeDeliveryServer } from "../runtime/shutdown.js";

test("repeated stop requests close every resource once, including failures", async () => {
  const codes: number[] = [];
  let calls = 0;
  const shutdown = createShutdown({ exit: (code) => { codes.push(code); }, error() {} });
  shutdown.add(() => { calls++; throw new Error("failed"); });
  shutdown.add(() => { calls++; assert.equal(shutdown.signal.aborted, true); });
  const first = shutdown.stop();
  assert.equal(shutdown.stop(), first);
  await first;
  assert.equal(calls, 2);
  assert.deepEqual(codes, [1]);
});

test("delivery shutdown closes clients with incomplete requests", async () => {
  const server = createServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address() as { port: number };
  const socket = connect(address.port, "127.0.0.1");
  try {
    await once(socket, "connect");
    socket.write("POST /deliver HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\nx");
    const closed = new Promise<void>((resolve) => socket.once("close", () => resolve()));
    socket.on("error", (error: NodeJS.ErrnoException) => { assert.equal(error.code, "ECONNRESET"); });
    await closeDeliveryServer(server);
    await closed;
  } finally {
    socket.destroy();
    server.close();
  }
});

for (const hung of [false, true]) {
  test(`SIGTERM exits without SIGKILL with ${hung ? "stalled" : "normal"} cleanup`, { timeout: 5000 }, async () => {
    const moduleURL = new URL("../runtime/shutdown.ts", import.meta.url).href;
    const script = `import { createShutdown } from ${JSON.stringify(moduleURL)};
      const shutdown = createShutdown({ exit: code => process.exit(code), error: () => {}, timeoutMs: 100 });
      shutdown.add(() => new Promise(resolve => ${hung ? "{}" : "setTimeout(resolve, 50)"}));
      process.on('SIGTERM', () => void shutdown.stop());
      setInterval(() => {}, 1000);
      console.log('ready');`;
    const child = spawn(process.execPath, ["--import", "tsx", "--input-type=module", "-e", script], { stdio: ["ignore", "pipe", "pipe"] });
    try {
      await once(child.stdout!, "data");
      const exited = once(child, "exit");
      child.kill("SIGTERM");
      setTimeout(() => child.kill("SIGTERM"), 20).unref();
      assert.deepEqual(await exited, [hung ? 1 : 0, null]);
    } finally { if (child.exitCode === null) child.kill("SIGKILL"); }
  });
}

test("a disposed voice controller cannot reconnect", async () => {
  const { createDiscordVoiceController } = await import("../voice/voice.js");
  const voice = createDiscordVoiceController({ token: "unused", voiceServiceEndpoint: "http://localhost" });
  await voice.dispose!();
  await voice.dispose!();
  await assert.rejects(voice.playRadio({ guildId: "1", userId: "2", streamUrl: "http://localhost" }), /closed/);
});
