import test from "node:test";
import assert from "node:assert/strict";
import { ACTIONS } from "../src/main/actions.js";
import { questionFor } from "../src/main/dialogs.js";
import { handleRequest } from "../src/main/ipc.js";
import { isWriteAction, needsConfirmation } from "../src/shared/actions.js";

const wrap = (body: unknown) => ({ domain: "runtime", result: body });
const limits = { defaultTimeoutMs: 1_000, maxTimeoutMs: 120_000 };

test("a model name is the only thing a pull or a removal carries, and nothing like a flag passes", () => {
  for (const type of ["runtime.models.pull", "runtime.models.remove"] as const) {
    assert.deepEqual(ACTIONS[type].payload({ model: "qwen3:8b", insecure: true }), { model: "qwen3:8b" });
    for (const model of ["", "--insecure", "-rm", "a b", "x;rm -rf /", 3, "x".repeat(129)]) assert.equal(ACTIONS[type].payload({ model }), undefined, String(model));
  }
});

test("downloading, removing, cancelling and starting the engine need a real press; only reading the status does not", () => {
  for (const type of ["runtime.models.pull", "runtime.models.remove", "runtime.models.cancel", "runtime.models.engine.start"] as const) assert.equal(isWriteAction(type), true, type);
  assert.equal(isWriteAction("runtime.models.status"), false);
  assert.equal(needsConfirmation("runtime.models.pull"), true);
  assert.equal(needsConfirmation("runtime.models.remove"), true);
  assert.equal(needsConfirmation("runtime.models.cancel"), false);
});

test("the shell's window names the model and warns about the size of a download", () => {
  const pull = questionFor("en", "runtime.models.pull", { model: "qwen3:8b" });
  assert.equal(pull?.message, "Download this model?");
  assert.match(pull?.detail ?? "", /^qwen3:8b\n\n.*gigabytes/s);
  assert.equal(pull?.accept, "Download");
  const remove = questionFor("pt", "runtime.models.remove", { model: "qwen3:8b" });
  assert.equal(remove?.message, "Remover este modelo?");
  assert.match(remove?.detail ?? "", /^qwen3:8b\n\n/);
});

test("a pull is refused when the person says no, and reaches the controller only after a yes", async () => {
  const calls: string[] = [];
  const client = { async request(type: string) { calls.push(type); return { status: "ok", result: wrap({ embedding_model: "bge-m3" }) }; } };
  const present = { recent: () => true };
  const no = await handleRequest({ type: "runtime.models.pull", payload: { model: "qwen3:8b" } }, client, limits, undefined, present, async () => false);
  assert.equal(no.ok, false);
  assert.deepEqual(calls, []);
  const yes = await handleRequest({ type: "runtime.models.pull", payload: { model: "qwen3:8b" } }, client, limits, undefined, present, async () => true);
  assert.equal(yes.ok, true);
  assert.deepEqual(calls, ["runtime.models.pull"]);
});

test("the models status keeps only known states and bounded text", () => {
  const status = ACTIONS["runtime.models.status"].project(wrap({
    embedding_model: "bge-m3", engine: "starting", private: "drop",
    pull: { model: "qwen3:8b", state: "pulling", phase: "downloading", percent: 42, error: "", raw: "engine text" },
  }));
  assert.deepEqual(status, { embeddingModel: "bge-m3", engine: "starting", pull: { model: "qwen3:8b", state: "pulling", phase: "downloading", percent: 42 } });
  const odd = ACTIONS["runtime.models.status"].project(wrap({ embedding_model: "bge-m3", engine: "exploded", pull: { model: "qwen3:8b", state: "failed", phase: "<b>", percent: 900, error: "x".repeat(500) } })) as { engine?: string; pull: { phase?: string; percent?: number; error: string } };
  assert.equal(odd.engine, undefined);
  assert.equal(odd.pull.phase, undefined);
  assert.equal(odd.pull.percent, undefined);
  assert.equal(odd.pull.error.length, 200);
  assert.throws(() => ACTIONS["runtime.models.status"].project(wrap({ embedding_model: "--x" })));
  assert.throws(() => ACTIONS["runtime.models.status"].project(wrap({ embedding_model: "bge-m3", pull: { model: "qwen3:8b", state: "exploded" } })));
});

test("the inventory keeps Ollama's sizes in the engine's shape and its embedding model", () => {
  const view = ACTIONS["runtime.providers"].project(wrap({ configuration_revision: 2, providers: [
    { id: "ollama", enabled: true, declared_models: [], state: "reachable", model_discovery: "listed", models: ["bge-m3:latest", "qwen3:8b"], model_sizes: { "bge-m3:latest": "1.2 GB", "qwen3:8b": "<b>big</b>", "--x": "1 GB" }, embedding_model: "bge-m3:latest" },
    { id: "codex", enabled: true, declared_models: [], state: "installed", model_sizes: { a: "1 GB" }, embedding_model: "a" },
  ] })) as { providers: Array<{ id: string; modelSizes?: Record<string, string>; embeddingModel?: string }> };
  const ollama = view.providers.find((row) => row.id === "ollama");
  assert.deepEqual(ollama?.modelSizes, { "bge-m3:latest": "1.2 GB" });
  assert.equal(ollama?.embeddingModel, "bge-m3:latest");
  const codex = view.providers.find((row) => row.id === "codex");
  assert.equal(codex?.modelSizes, undefined);
  assert.equal(codex?.embeddingModel, undefined);
});
