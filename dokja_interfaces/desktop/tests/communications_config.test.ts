import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { communicationsToken } from "../src/main/communications_config.js";
import { parseConfig } from "../src/main/config.js";

function fixture(t: test.TestContext): string {
  const root = mkdtempSync(join(tmpdir(), "dokja-communications-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  return root;
}

test("Desktop reads the existing checkout credential without exporting other variables", (t) => {
  const root = fixture(t);
  writeFileSync(join(root, ".env"), `DOKJA_COMMUNICATIONS_TOKEN="${"a".repeat(32)}" # private\nUNRELATED_SECRET=fixture\n`);
  const env = {};
  assert.equal(communicationsToken(env, root), "a".repeat(32));
  assert.deepEqual(env, {});
});

test("explicit process values, including blank values, take precedence", (t) => {
  const root = fixture(t);
  writeFileSync(join(root, ".env"), "DOKJA_COMMUNICATIONS_TOKEN=file-value\n");
  assert.equal(communicationsToken({ DOKJA_COMMUNICATIONS_TOKEN: "shell-value" }, root), "shell-value");
  assert.equal(communicationsToken({ DOKJA_COMMUNICATIONS_TOKEN: "" }, root), "");
});

test("an explicit production file is selected without falling back to another stack", (t) => {
  const root = fixture(t);
  writeFileSync(join(root, ".env"), "DOKJA_COMMUNICATIONS_TOKEN=development\n");
  const prod = join(root, ".env.prod");
  writeFileSync(prod, "export DOKJA_COMMUNICATIONS_TOKEN='production'\n");
  const config = parseConfig(["--env-file", prod], {}, "en");
  assert.equal(config.envFile, prod);
  assert.equal(communicationsToken({}, root, config.envFile), "production");
  writeFileSync(prod, "UNRELATED_SECRET=fixture\n");
  assert.equal(communicationsToken({}, root, prod), undefined);
});

test("duplicate assignments use the final value and shell expressions remain literal", (t) => {
  const root = fixture(t);
  writeFileSync(join(root, ".env"), "DOKJA_COMMUNICATIONS_TOKEN=old\nDOKJA_COMMUNICATIONS_TOKEN='$(do-not-execute)'\n");
  assert.equal(communicationsToken({}, root), "$(do-not-execute)");
});

test("missing defaults remain unconfigured and explicit read errors reveal no private input", (t) => {
  const root = fixture(t);
  assert.equal(communicationsToken({}), undefined);
  assert.equal(communicationsToken({}, root), undefined);
  assert.throws(() => communicationsToken({}, root, join(root, "missing")), {
    message: "Could not read the desktop communications env file.",
  });
});
