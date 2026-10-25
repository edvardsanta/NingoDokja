import { expect, it } from "vitest";

import { describeError, knownFailure, needsWrittenText } from "../../src/renderer/cards/card_frame.js";
import { createTranslator as translator } from "../../src/renderer/i18n/i18n.js";

const orchestrator = (message: string) => ({ code: "orchestrator" as const, message });

it("says a missing image reader and a refused download plainly, in both languages", () => {
  const missing = orchestrator('dispatch workflow: dispatch meme: unavailable: no enabled provider of media.perceive.v1 for "read-text"');
  const blocked = orchestrator("dispatch workflow: dispatch system: delivered to []: send to channel 1: discord delivery interface returned status 500: attachment fetch failed (403)");
  expect(describeError(missing, translator("en"))).toMatch(/^Reading images is not installed/);
  expect(describeError(missing, translator("pt"))).toMatch(/^A leitura de imagens não está instalada/);
  expect(describeError(blocked, translator("en"))).toMatch(/^The image's site refused the download \(403\)/);
  expect(needsWrittenText(missing)).toBe(true);
  expect(needsWrittenText(orchestrator("no text was found in the media at x; provide explicit text"))).toBe(true);
  expect(needsWrittenText(blocked)).toBe(false);
});

it("leaves every other failure in the orchestrator's words", () => {
  expect(knownFailure(orchestrator("something else"))).toBeUndefined();
  expect(knownFailure({ code: "timeout", message: "no enabled provider of media.perceive.v1" })).toBeUndefined();
  expect(describeError(orchestrator("something else"), translator("en"))).toBe("The orchestrator answered with an error: something else");
});
