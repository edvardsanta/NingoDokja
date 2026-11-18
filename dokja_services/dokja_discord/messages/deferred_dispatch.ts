import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { DatabaseSync } from "node:sqlite";

import type { BridgeRequest } from "../transport/bridge.js";

// A deferred dispatch: a message the orchestrator could not take when it arrived,
// waiting for its late retry. The job persists beside the other runtime databases
// so the promised return outlives a container restart; the timers the listener
// arms on top are only ephemeral triggers for the stored next_retry_at.
export type DeferredDispatchJob = {
  messageId: string;
  channelId: string;
  userId: string;
  // The exact bridge payload the retry re-sends; the deterministic session key
  // keeps the late turn in the conversation the message started.
  body: BridgeRequest;
  // Retries already spent; the next one indexes the backoff schedule.
  attempt: number;
  nextRetryAt: number;
};

export type DeferredDispatchStore = {
  upsert(job: DeferredDispatchJob): void;
  delete(messageId: string): void;
  // Every pending job, soonest first; the scheduler re-arms each one on boot.
  pending(): DeferredDispatchJob[];
  close(): void;
};

// openDeferredDispatchStore keeps the queue in a plain SQLite file. ":memory:"
// suits tests; the application passes a path beside the runtime databases.
export function openDeferredDispatchStore(path: string): DeferredDispatchStore {
  if (path !== ":memory:") {
    mkdirSync(dirname(path), { recursive: true });
  }
  const db = new DatabaseSync(path);
  db.exec(`CREATE TABLE IF NOT EXISTS deferred_dispatches (
    message_id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    body TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    next_retry_at INTEGER NOT NULL
  )`);
  const upsertRow = db.prepare(`INSERT INTO deferred_dispatches
    (message_id, channel_id, user_id, body, attempt, next_retry_at)
    VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT(message_id) DO UPDATE SET
      channel_id = excluded.channel_id,
      user_id = excluded.user_id,
      body = excluded.body,
      attempt = excluded.attempt,
      next_retry_at = excluded.next_retry_at`);
  const deleteRow = db.prepare("DELETE FROM deferred_dispatches WHERE message_id = ?");
  const selectRows = db.prepare("SELECT * FROM deferred_dispatches ORDER BY next_retry_at");
  return {
    upsert(job: DeferredDispatchJob) {
      upsertRow.run(job.messageId, job.channelId, job.userId, JSON.stringify(job.body), job.attempt, job.nextRetryAt);
    },
    delete(messageId: string) {
      deleteRow.run(messageId);
    },
    pending(): DeferredDispatchJob[] {
      const rows = selectRows.all() as Array<Record<string, unknown>>;
      return rows.map((row) => ({
        messageId: String(row.message_id),
        channelId: String(row.channel_id),
        userId: String(row.user_id),
        body: JSON.parse(String(row.body)) as BridgeRequest,
        attempt: Number(row.attempt),
        nextRetryAt: Number(row.next_retry_at),
      }));
    },
    close() {
      db.close();
    },
  };
}
