import { afterEach, describe, expect, it } from "vitest";

import { loadSession } from "../../src/renderer/session.js";
import type { Bootstrap, Transport } from "../../src/shared/transport.js";

type Shell = Transport & { bootstrap(): Promise<Bootstrap | null> };

function installShell(bootstrap: Bootstrap | null): Shell {
  const shell: Shell = {
    request: async () => ({ ok: true, result: "from the shell" }),
    bootstrap: async () => bootstrap,
  };
  window.dokja = shell;
  return shell;
}

afterEach(() => {
  delete window.dokja;
});

describe("loadSession", () => {
  it("uses the shell's transport and the language it reports", async () => {
    const shell = installShell({ locale: "pt" });
    const session = await loadSession();

    expect(session.transport).toBe(shell);
    expect(session.locale).toBe("pt");
  });

  it("falls back to English when the shell does not trust the page", async () => {
    installShell(null);
    expect((await loadSession()).locale).toBe("en");
  });

  it("in development, without a shell, answers the health card from made-up data", async () => {
    const session = await loadSession();

    const status = await session.transport.request("ningo.status", {});
    expect(status.ok).toBe(true);
    expect(JSON.stringify(status)).toContain("chat_ai");
  });
});
