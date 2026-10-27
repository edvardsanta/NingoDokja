import { Reply, Router } from "zeromq";

export type ReplyServer = {
  endpoint: string;
  requests: Array<Record<string, unknown>>;
  close(): Promise<void>;
};

export const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

export const ok = (result: unknown): string => JSON.stringify({ status: "ok", result });

// A real REP socket on a free loopback port, standing in for the orchestrator. It records every
// request it receives and answers with whatever the handler returns.
export async function startReplyServer(
  handle: (request: Record<string, unknown>, index: number) => Promise<string> | string,
): Promise<ReplyServer> {
  const socket = new Reply();
  await socket.bind("tcp://127.0.0.1:*");
  const requests: Array<Record<string, unknown>> = [];

  const finished = (async () => {
    try {
      for await (const [frame] of socket) {
        const request = JSON.parse(Buffer.from(frame ?? new Uint8Array()).toString("utf8"));
        requests.push(request);
        await socket.send(await handle(request, requests.length));
      }
    } catch {
      // the socket was closed while waiting
    }
  })();

  return {
    endpoint: socket.lastEndpoint ?? "",
    requests,
    async close() {
      socket.close();
      await finished;
    },
  };
}

// A ROUTER socket answering every request as soon as its handler is done, in any order, as the
// orchestrator's request workers do.
export async function startConcurrentServer(
  handle: (request: Record<string, unknown>) => Promise<string> | string,
): Promise<ReplyServer> {
  const socket = new Router();
  await socket.bind("tcp://127.0.0.1:*");
  const requests: Array<Record<string, unknown>> = [];
  const pending = new Set<Promise<void>>();
  let sending = Promise.resolve();

  const finished = (async () => {
    try {
      for await (const [identity, empty, frame] of socket) {
        const request = JSON.parse(Buffer.from(frame ?? new Uint8Array()).toString("utf8"));
        requests.push(request);
        const answering = (async () => {
          const reply = await handle(request);
          // one send at a time on the socket, whatever order the answers come in
          sending = sending.then(() => socket.send([identity!, empty ?? new Uint8Array(), reply])).catch(() => {});
          await sending;
        })();
        pending.add(answering);
        void answering.finally(() => pending.delete(answering));
      }
    } catch {
      // the socket was closed while waiting
    }
  })();

  return {
    endpoint: socket.lastEndpoint ?? "",
    requests,
    async close() {
      await Promise.allSettled([...pending]);
      socket.close();
      await finished;
    },
  };
}
