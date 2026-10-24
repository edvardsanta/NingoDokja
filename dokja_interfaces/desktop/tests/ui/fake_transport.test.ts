import { afterEach, expect, it, vi } from "vitest";
import { createFakeTransport } from "../../src/renderer/fake_transport.js";
import type { ChatPreview } from "../../src/shared/transport.js";

afterEach(() => vi.useRealTimers());

it("keeps local model controls working while the demo streams a chat reply", async () => {
  vi.useFakeTimers();
  const transport = createFakeTransport(0);
  const previews: ChatPreview[] = [];
  const unsubscribe = transport.onChatPreview!((preview) => previews.push(preview));
  const reply = transport.request("message.created", { conversation_id: "demo-chat" });
  const providers = transport.request("runtime.providers");
  const selection = transport.request("runtime.chat.configure", {
    provider: "ollama", model: "demo/small:latest",
  });

  await vi.advanceTimersByTimeAsync(500);
  expect(previews.length).toBeGreaterThan(0);
  expect(previews.every((preview) => preview.conversationId === "demo-chat")).toBe(true);
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
