import { afterEach, expect, it, vi } from "vitest";
import { createFakeTransport } from "../../src/renderer/fake_transport.js";
import type { ChatPreview } from "../../src/shared/transport.js";

afterEach(() => vi.useRealTimers());

it("keeps local model controls working while the demo streams a chat reply", async () => {
  vi.useFakeTimers();
  const transport = createFakeTransport(0);
  const previews: ChatPreview[] = [];
  const unsubscribe = transport.onChatPreview!((preview) => previews.push(preview));
  const starting = transport.request("conversation.start", {});
  await vi.advanceTimersByTimeAsync(0);
  const started = await starting;
  const conversationId = started.ok ? String((started.result as { conversation: { id: string } }).conversation.id) : "";
  const reply = transport.request("message.created", { conversation_id: conversationId, content: "hello", send_id: "send-0001" });
  const providers = transport.request("runtime.providers");
  const selection = transport.request("runtime.chat.configure", {
    provider: "ollama", model: "demo/small:latest",
  });

  await vi.advanceTimersByTimeAsync(500);
  expect(previews.length).toBeGreaterThan(0);
  expect(previews.every((preview) => preview.conversationId === conversationId)).toBe(true);
  await expect(providers).resolves.toMatchObject({ ok: true, result: {
    providers: [expect.objectContaining({ id: "ollama", models: expect.arrayContaining(["demo/small:latest"]) }), expect.anything()],
  } });
  await expect(selection).resolves.toMatchObject({ ok: true, result: {
    provider: "ollama", model: "demo/small:latest",
  } });

  unsubscribe();
  const count = previews.length;
  await vi.runAllTimersAsync();
  await expect(reply).resolves.toMatchObject({ ok: true, result: {
    reply: expect.stringContaining(previews[count - 1]!.text),
  } });
  expect(previews).toHaveLength(count);
});

it("reads a post link and saves it, so the Post way can be tried without a shell", async () => {
  const transport = createFakeTransport(0);
  const link = "https://example.com/p/1";

  const read = await transport.request("digest.link.read", { url: link });
  expect(read).toMatchObject({ ok: true, result: { status: "ok", post: { url: link, author: "Example Author" } } });
  const { documentId, media } = (read as { result: { post: { documentId: string; media: unknown[] } } }).result.post;
  expect(documentId).toMatch(/^post-[0-9a-f]{16}$/);
  expect(media).toHaveLength(2);
  // the same link is the same post, so saving it twice updates it
  const again = await transport.request("digest.link.read", { url: link });
  expect(again).toMatchObject({ ok: true, result: { post: { documentId } } });

  await expect(transport.request("knowledge.ingest", { mode: "post", post: documentId })).resolves.toMatchObject({
    ok: true, result: { created: 1, updated: 0 },
  });
  await expect(transport.request("knowledge.ingest", { mode: "post", post: documentId })).resolves.toMatchObject({
    ok: true, result: { created: 0, updated: 1 },
  });
  await expect(transport.request("knowledge.ingest", { mode: "post", post: "post-0000000000000000" })).resolves.toMatchObject({
    ok: false, error: { code: "orchestrator" },
  });
  await expect(transport.openSavedPost?.(documentId)).resolves.toMatchObject({ ok: true });
});

it("refuses the post links whose path names a refusal, and shows a post with no files for /text", async () => {
  const transport = createFakeTransport(0);
  const asked = async (path: string) => (await transport.request("digest.link.read", { url: `https://example.com/${path}` })) as { result: Record<string, unknown> };

  expect((await asked("login")).result).toEqual({ status: "refused", refused: "login_required" });
  expect((await asked("private")).result).toEqual({ status: "refused", refused: "unavailable" });
  expect((await asked("page")).result).toEqual({ status: "refused", refused: "unsupported" });
  expect((await asked("broken")).result).toEqual({ status: "refused", refused: "failed" });
  expect((await asked("reader")).result).toEqual({ status: "refused", refused: "no_reader" });
  expect((await asked("text")).result).toMatchObject({ status: "ok", post: { media: [] } });
});
