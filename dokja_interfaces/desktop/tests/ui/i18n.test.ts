import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { CATALOGS, createTranslator } from "../../src/renderer/i18n/i18n.js";
import en from "../../src/renderer/i18n/locales/en.json";
import pt from "../../src/renderer/i18n/locales/pt.json";

const RENDERER = join(__dirname, "../../src/renderer");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "locales" ? [] : sources(path);
    return /\.(ts|tsx)$/.test(name) ? [readFileSync(path, "utf8")] : [];
  });
}

const placeholders = (message: string) => [...message.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe("the catalogs", () => {
  it("have the same message ids", () => {
    expect(Object.keys(pt).sort()).toEqual(Object.keys(en).sort());
  });

  it("have no empty message and keep the same placeholders in every language", () => {
    for (const [id, message] of Object.entries(en)) {
      expect(message.trim(), `en ${id}`).not.toBe("");
      expect((pt as Record<string, string>)[id]?.trim(), `pt ${id}`).toBeTruthy();
      expect(placeholders((pt as Record<string, string>)[id] ?? ""), `placeholders of ${id}`).toEqual(
        placeholders(message),
      );
    }
  });

  it("have no id the screen does not use, so nothing goes stale", () => {
    const code = sources(RENDERER).join("\n");
    const unused = Object.keys(en).filter((id) => !code.includes(`"${id}"`));
    expect(unused).toEqual([]);
  });

  // Words that are the same in both languages by nature, not because a message was forgotten.
  const SAME_IN_BOTH = new Set(["kind_memes"]);

  it("translate every message, so none was left in English", () => {
    const same = Object.keys(en).filter(
      (id) => !SAME_IN_BOTH.has(id) && (en as Record<string, string>)[id] === (pt as Record<string, string>)[id],
    );
    expect(same).toEqual([]);
  });

  it("keep Portuguese out of the English catalog", () => {
    for (const [id, message] of Object.entries(en)) {
      expect(message, id).not.toMatch(/[áàâãäéèêíóôõúüç]/i);
    }
  });
});

describe("the translator", () => {
  it("fills placeholders and leaves unknown ones as they are", () => {
    const t = createTranslator("en");
    expect(t("health_summary", { ok: 2, problem: 1, off: 0 })).toBe("2 up, 1 with problems, 0 switched off");
    expect(t("error_orchestrator", {})).toBe("The orchestrator answered with an error: {message}");
  });

  it("speaks Portuguese when asked", () => {
    const t = createTranslator("pt");
    expect(t("status_ok")).toBe("no ar");
    expect(t("health_title")).toBe("Serviços");
  });

  it("falls back to English for a message another language lacks, never to the id", () => {
    const catalog = CATALOGS.pt;
    const saved = catalog.refresh;
    delete catalog.refresh;
    try {
      expect(createTranslator("pt")("refresh")).toBe("Refresh");
    } finally {
      catalog.refresh = saved as string;
    }
  });
});
