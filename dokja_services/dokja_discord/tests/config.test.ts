import assert from "node:assert/strict";
import test from "node:test";

import { buildConfig } from "../config.js";

function withEnv(values: Record<string, string | undefined>, run: () => void) {
  const saved = Object.fromEntries(Object.keys(values).map((key) => [key, process.env[key]]));
  try {
    for (const [key, value] of Object.entries(values)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    run();
  } finally {
    for (const [key, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

test("DISCORD_GUILD_ID names one server or several, separated by commas", () => {
  withEnv({ DISCORD_GUILD_ID: " first , second ,," }, () => {
    assert.deepEqual(buildConfig().allowedGuildIds, ["first", "second"]);
  });
  withEnv({ DISCORD_GUILD_ID: "only" }, () => {
    assert.deepEqual(buildConfig().allowedGuildIds, ["only"]);
  });
  withEnv({ DISCORD_GUILD_ID: undefined }, () => {
    assert.deepEqual(buildConfig().allowedGuildIds, []);
  });
});
