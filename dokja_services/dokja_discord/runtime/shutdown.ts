import type { Server } from "node:http";

// Register resources before awaiting startup, so even a pending login can stop.
export function createShutdown(options: {
  exit(code: number): void;
  error(message: string): void;
  timeoutMs?: number;
}) {
  const controller = new AbortController();
  const cleanups: Array<() => unknown> = [];
  let stopping: Promise<void> | undefined;
  return {
    signal: controller.signal,
    add(cleanup: () => unknown) { cleanups.push(cleanup); },
    stop(exitCode = 0): Promise<void> {
      if (stopping) return stopping;
      // Assign the promise before invoking callbacks: reentrant signals are safe.
      stopping = Promise.resolve().then(async () => {
        controller.abort();
        const deadline = setTimeout(() => {
          options.error("Shutdown deadline exceeded");
          options.exit(1);
        }, options.timeoutMs ?? 8_000);
        const results = await Promise.allSettled(cleanups.map((cleanup) => Promise.resolve().then(cleanup)));
        clearTimeout(deadline);
        const failed = results.some((result) => result.status === "rejected");
        if (failed) options.error("Resource cleanup failed");
        options.exit(failed ? 1 : exitCode);
      });
      return stopping;
    },
  };
}

export type Shutdown = ReturnType<typeof createShutdown>;

export function closeDeliveryServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
    // Container shutdown does not accept new deliveries or wait for slow clients.
    server.closeAllConnections();
  });
}
