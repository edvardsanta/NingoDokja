import test from "node:test";
import assert from "node:assert/strict";

import { createCommands } from "../commands/commands.js";
import { serializeCommand } from "../runtime/discordjs_runtime.js";

// Discord's rules for what a command may be called.
const NAME = /^[-_\p{Ll}\p{Lo}\p{N}]{1,32}$/u;

test("every command, option and choice has a Portuguese name and description Discord accepts", () => {
  for (const command of createCommands("http://orch", 1000).map(serializeCommand)) {
    const where = `/${command.name}`;
    assert.match(command.name, NAME, where);
    assert.ok(command.description.length >= 1 && command.description.length <= 100, where);
    assert.match(command.name_localizations?.["pt-BR"] ?? "", NAME, `${where} pt-BR name`);
    const description = command.description_localizations?.["pt-BR"] ?? "";
    assert.ok(description.length >= 1 && description.length <= 100, `${where} pt-BR description`);
    for (const option of (command.options ?? []) as any[]) {
      const at = `${where} ${option.name}`;
      assert.match(option.name, NAME, at);
      assert.match(option.name_localizations?.["pt-BR"] ?? "", NAME, `${at} pt-BR name`);
      const optionDescription = option.description_localizations?.["pt-BR"] ?? "";
      assert.ok(optionDescription.length >= 1 && optionDescription.length <= 100, `${at} pt-BR description`);
      for (const choice of option.choices ?? []) {
        const label = choice.name_localizations?.["pt-BR"] ?? "";
        assert.ok(label.length >= 1 && label.length <= 100, `${at} choice ${choice.value}`);
      }
    }
  }
});

test("registration keeps what each option declares, translations, bounds and choices included", () => {
  const commands = createCommands("http://orch", 1000).map(serializeCommand);
  const price = commands.find((command) => command.name === "price")!;
  assert.equal(price.name_localizations?.["pt-BR"], "preco");
  const store = (price.options as any[]).find((option) => option.name === "store");
  assert.equal(store.autocomplete, true);
  assert.equal(store.name_localizations["pt-BR"], "loja");
  const voice = commands.find((command) => command.name === "voice_chat_start")!;
  const mode = (voice.options as any[])[0];
  assert.deepEqual(mode.choices.map((choice: any) => [choice.value, choice.name_localizations["pt-BR"]]),
    [["always", "Sempre"], ["wakeword", "Só depois da palavra de chamada"]]);
  // an integer option keeps its type and bounds (the old registration dropped integer options)
  const integer = serializeCommand({ name: "x", description: "x", options: [
    { name: "n", description: "n", type: 4, required: true, min_value: 0, max_value: 100 },
  ], handle: async () => {} });
  assert.deepEqual(integer.options, [{ type: 4, name: "n", description: "n", required: true, min_value: 0, max_value: 100 }]);
});
