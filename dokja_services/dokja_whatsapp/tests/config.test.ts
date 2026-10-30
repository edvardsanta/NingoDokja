import assert from "node:assert/strict";
import test from "node:test";

import { buildConfig, equivalentNumbers, normalizeNumber, numberAllowed } from "../config.js";

const base = { DOKJA_WHATSAPP_AUTH_DIR: "/auth/default" };

test("the session folder is required", () => {
  assert.throws(() => buildConfig({}), /DOKJA_WHATSAPP_AUTH_DIR/);
});

test("defaults answer only the account's own chat", () => {
  const config = buildConfig(base);

  assert.equal(config.authDir, "/auth/default");
  assert.equal(config.selfChat, true);
  assert.deepEqual(config.allowedNumbers, []);
  assert.deepEqual(config.allowedGroups, []);
  assert.equal(config.orchestratorEndpoint, "http://dokja-orchestrator:8091/orchestrator");
});

test("the status port defaults and is validated", () => {
  assert.equal(buildConfig(base).statusPort, 8094);
  assert.equal(buildConfig({ ...base, DOKJA_WHATSAPP_STATUS_PORT: "9001" }).statusPort, 9001);
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_STATUS_PORT: "70000" }), /STATUS_PORT must be a port number/);
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_STATUS_PORT: "abc" }), /STATUS_PORT must be a port number/);
});

test("numbers are normalised and listed once", () => {
  const config = buildConfig({
    ...base,
    DOKJA_WHATSAPP_ALLOW_FROM: "+55 (11) 91234-5678, 5511912345678 ,+1 202 555 0100",
  });

  assert.deepEqual(config.allowedNumbers, ["5511912345678", "12025550100"]);
});

test("an invalid number stops the start-up without echoing it", () => {
  assert.throws(
    () => buildConfig({ ...base, DOKJA_WHATSAPP_ALLOW_FROM: "5511912345678,not-a-number" }),
    (error: Error) => /entry 2/.test(error.message) && !error.message.includes("not-a-number"),
  );
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_ALLOW_FROM: "123" }), /entry 1/);
});

test("groups must be group ids", () => {
  const config = buildConfig({ ...base, DOKJA_WHATSAPP_ALLOWED_GROUPS: "120363000000000001@g.us, 5511999999999-1600000000@g.us" });
  assert.deepEqual(config.allowedGroups, ["120363000000000001@g.us", "5511999999999-1600000000@g.us"]);

  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_ALLOWED_GROUPS: "5511912345678@s.whatsapp.net" }), /entry 1/);
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_ALLOWED_GROUPS: "team chat" }), /entry 1/);
});

test("self chat can be switched off but something must stay allowed", () => {
  assert.equal(buildConfig({ ...base, DOKJA_WHATSAPP_SELF_CHAT: "false", DOKJA_WHATSAPP_ALLOW_FROM: "5511912345678" }).selfChat, false);
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_SELF_CHAT: "false" }), /Nothing would be answered/);
  assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_SELF_CHAT: "maybe" }), /must be true or false/);
});

test("normalizeNumber keeps only plausible phone numbers", () => {
  assert.equal(normalizeNumber("+55 11 91234-5678"), "5511912345678");
  assert.equal(normalizeNumber("12345"), "");
  assert.equal(normalizeNumber("1234567890123456"), "");
  assert.equal(normalizeNumber("55119x234"), "");
});

test("a Brazilian mobile matches with or without the ninth digit", () => {
  assert.deepEqual(equivalentNumbers("5511987654321"), ["5511987654321", "551187654321"]);
  assert.deepEqual(equivalentNumbers("551187654321"), ["5511987654321", "551187654321"]);
  // Other countries, Brazilian landlines and the newer mobile ranges keep a single spelling.
  assert.deepEqual(equivalentNumbers("12025550100"), ["12025550100"]);
  assert.deepEqual(equivalentNumbers("551132345678"), ["551132345678"]);
  assert.deepEqual(equivalentNumbers("5511912345678"), ["5511912345678"]);
});

test("numberAllowed matches any known number of the sender", () => {
  assert.equal(numberAllowed(["5511987654321"], ["551187654321"]), true);
  assert.equal(numberAllowed(["551187654321"], ["5511987654321"]), true);
  assert.equal(numberAllowed(["5511912345678"], ["5511987654321", "12025550100"]), false);
  assert.equal(numberAllowed([], ["5511912345678"]), false);
  assert.equal(numberAllowed(["5511912345678"], []), false);
});

test("no conversation is named by default, so nothing is recorded", () => {
  const config = buildConfig(base);

  assert.deepEqual(config.conversations, []);
  assert.equal(config.historyDays, 30);
});

test("named conversations keep an alias and either a number or a group id", () => {
  const config = buildConfig({
    ...base,
    DOKJA_WHATSAPP_CONVERSATIONS: "friend=+55 11 98765-4321, team-chat=120363000000000001@g.us",
    DOKJA_WHATSAPP_HISTORY_DIR: "/history",
    DOKJA_WHATSAPP_HISTORY_DAYS: "7",
  });

  assert.deepEqual(config.conversations, [
    { alias: "friend", number: "5511987654321" },
    { alias: "team-chat", group: "120363000000000001@g.us" },
  ]);
  assert.equal(config.historyDir, "/history");
  assert.equal(config.historyDays, 7);
});

test("a bad conversation entry names its position and never its value", () => {
  const history = { ...base, DOKJA_WHATSAPP_HISTORY_DIR: "/history" };
  for (const [raw, pattern] of [
    ["no-separator", /entry 1 needs an alias/],
    ["Friend=5511987654321", /entry 1 needs an alias/],
    ["friend=not-a-number", /entry 1 is not a phone number or a group id/],
    ["ok=5511987654321,ok=5511912345678", /entry 2 repeats/],
    ["a=5511987654321,b=551187654321", /entry 2 repeats/],
  ] as const) {
    assert.throws(
      () => buildConfig({ ...history, DOKJA_WHATSAPP_CONVERSATIONS: raw }),
      (error: Error) => pattern.test(error.message) && !/5511987654321|not-a-number/.test(error.message),
      raw,
    );
  }
});

test("named conversations need a folder for their history, and the days are bounded", () => {
  assert.throws(
    () => buildConfig({ ...base, DOKJA_WHATSAPP_CONVERSATIONS: "friend=5511987654321" }),
    /DOKJA_WHATSAPP_HISTORY_DIR/,
  );
  for (const days of ["0", "366", "abc", "1.5"]) {
    assert.throws(() => buildConfig({ ...base, DOKJA_WHATSAPP_HISTORY_DAYS: days }), /number of days from 1 to 365/, days);
  }
});

test("WhatsApp locale defaults to Brazilian Portuguese and can be configured",()=>{
 assert.equal(buildConfig(base).locale,"pt-BR");
 assert.equal(buildConfig({...base,DOKJA_WHATSAPP_LOCALE:"en"}).locale,"en");
});
