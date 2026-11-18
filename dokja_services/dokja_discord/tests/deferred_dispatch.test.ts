import test from "node:test";
import assert from "node:assert/strict";

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { buildBridgePayload } from "../transport/bridge.js";
import { openDeferredDispatchStore, type DeferredDispatchJob } from "../messages/deferred_dispatch.js";

function job(overrides: Partial<DeferredDispatchJob> = {}): DeferredDispatchJob {
  return {
    messageId: "m1",
    channelId: "c",
    userId: "u",
    attempt: 1,
    nextRetryAt: 123,
    body: buildBridgePayload({ eventType: "message.created", userId: "u", channelId: "c", messageId: "m1" }),
    ...overrides,
  };
}

test("deferred dispatches persist across a close and reopen", () => {
  const dir = mkdtempSync(join(tmpdir(), "dokja-deferral-"));
  const dbPath = join(dir, "deferrals.db");
  try {
    const before = openDeferredDispatchStore(dbPath);
    before.upsert(job());
    before.upsert(job({ messageId: "m2", attempt: 2, nextRetryAt: 456 }));
    before.close();

    const after = openDeferredDispatchStore(dbPath);
    const pending = after.pending();
    assert.equal(pending.length, 2);
    assert.equal(pending[0].messageId, "m1");
    assert.equal(pending[0].body.event_type, "message.created");
    assert.equal(pending[1].attempt, 2);
    after.delete("m1");
    assert.equal(after.pending().length, 1);
    after.close();
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("upsert replaces the job of the same message", () => {
  const store = openDeferredDispatchStore(":memory:");
  store.upsert(job({ attempt: 1, nextRetryAt: 1 }));
  store.upsert(job({ attempt: 2, nextRetryAt: 2 }));
  const pending = store.pending();
  assert.equal(pending.length, 1);
  assert.equal(pending[0].attempt, 2);
  store.close();
});
