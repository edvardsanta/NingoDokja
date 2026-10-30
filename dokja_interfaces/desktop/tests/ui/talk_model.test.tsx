import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AssistantCard } from "../../src/renderer/cards/assistant.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { RuntimeEnabled } from "../../src/renderer/runtime_context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport } from "../../src/shared/transport.js";
import { failure, ok, routedTransport } from "./support.js";

const provider = (id: string, name: string, declaredModels: string[], enabled = true) =>
  ({ id, name, state: "installed", reason: "", models: [], enabled, declaredModels, modelDiscovery: "unsupported", inferenceStatus: "not_tested", truncated: false });
const providers = ok({ configurationRevision: 4, checkedAt: "", providers: [
  provider("codex", "Codex", ["gpt-a", "gpt-b"]),
  provider("ollama", "Ollama", ["local-x"], false),
] });
const binding = (revision: number, provider: string, model: string) => ok({ configurationRevision: revision, provider, model });

function renderTalk(transport: Transport, runtime = true, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <RuntimeEnabled.Provider value={runtime}><AssistantCard transport={transport} /></RuntimeEnabled.Provider>
    </I18nProvider>,
  );
}

const picker = () => screen.findByRole("combobox", { name: "Model" }) as Promise<HTMLSelectElement>;
// The list of models is read when the owner goes to choose.
const opened = async () => {
  const select = await picker();
  fireEvent.focus(select);
  await waitFor(() => expect(select.options.length).toBeGreaterThan(1));
  return select;
};

describe("choosing the model in Talk", () => {
  it("shows the model that answers and offers only the enabled providers' declared models", async () => {
    const { transport } = routedTransport({ "conversation.list": failure("unavailable"), "runtime.providers": providers, "runtime.chat": binding(7, "codex", "gpt-a") });
    renderTalk(transport);
    const select = await opened();
    expect(select.value).toBe("codex|gpt-a");
    expect([...select.options].map((option) => option.textContent)).toEqual(["Codex / gpt-a", "Codex / gpt-b"]);
    expect(select.closest(".wp-dock")).not.toBeNull();
  });

  it("sits in the quiet row under the field, beside the hint, inside the docked form", async () => {
    const { transport } = routedTransport({ "conversation.list": failure("unavailable"), "runtime.providers": providers, "runtime.chat": binding(7, "codex", "gpt-a") });
    renderTalk(transport);
    const select = await picker();
    const foot = select.closest(".composer-foot");
    expect(foot).not.toBeNull();
    expect(foot?.querySelector(".composer-hint")?.textContent).toBe("Enter sends · Shift+Enter breaks the line");
    expect(foot?.closest("form")?.contains(screen.getByRole("textbox", { name: "Your message" }))).toBe(true);
    // Its label stays visible: only the field's own label is for screen readers.
    expect(screen.getByText("Model").closest(".composer > label")).toBeNull();
  });

  it("switches at once, naming the configuration it read just before", async () => {
    const { transport, calls } = routedTransport({
      "conversation.list": failure("unavailable"), "runtime.providers": providers,
      "runtime.chat": [binding(7, "codex", "gpt-a"), binding(8, "codex", "gpt-a")],
      "runtime.chat.configure": binding(9, "codex", "gpt-b"),
    });
    renderTalk(transport);
    fireEvent.change(await opened(), { target: { value: "codex|gpt-b" } });
    await waitFor(async () => expect((await picker()).value).toBe("codex|gpt-b"));
    expect(calls.find((call) => call.type === "runtime.chat.configure")?.payload).toEqual({ configuration_revision: 8, provider: "codex", model: "gpt-b" });
  });

  it("keeps the model that answers and says why when the switch is refused", async () => {
    const { transport } = routedTransport({
      "conversation.list": failure("unavailable"), "runtime.providers": providers, "runtime.chat": binding(7, "codex", "gpt-a"),
      "runtime.chat.configure": failure("orchestrator", "selected chat provider is disabled"),
    });
    renderTalk(transport);
    fireEvent.change(await opened(), { target: { value: "codex|gpt-b" } });
    await screen.findByRole("alert");
    expect((await picker()).value).toBe("codex|gpt-a");
  });

  it("points to System when no model is declared, in Portuguese too", async () => {
    const { transport } = routedTransport({
      "conversation.list": failure("unavailable"),
      "runtime.providers": ok({ configurationRevision: 1, checkedAt: "", providers: [provider("codex", "Codex", [])] }),
      "runtime.chat": binding(1, "", ""),
    });
    renderTalk(transport, true, "pt");
    fireEvent.focus(await screen.findByRole("combobox", { name: "Modelo" }));
    await screen.findByText("Nenhum modelo de conversa foi declarado ainda.");
    screen.getByRole("button", { name: "Abrir Sistema" });
  });

  it("reads only the model that answers until the owner goes to choose", async () => {
    const { transport, calls } = routedTransport({ "conversation.list": failure("unavailable"), "runtime.providers": providers, "runtime.chat": binding(7, "codex", "gpt-a") });
    renderTalk(transport);
    const select = await picker();
    expect([...select.options].map((option) => option.textContent)).toEqual(["codex / gpt-a"]);
    expect(calls.filter((call) => call.type === "runtime.providers")).toHaveLength(0);
  });

  it("is not offered, and asks nothing, when the runtime controls are off", async () => {
    const { transport, calls } = routedTransport({ "conversation.list": failure("unavailable") });
    renderTalk(transport, false);
    await screen.findByRole("textbox", { name: "Your message" });
    expect(screen.queryByRole("combobox", { name: "Model" })).toBeNull();
    expect(calls.filter((call) => call.type.startsWith("runtime."))).toHaveLength(0);
  });
});
