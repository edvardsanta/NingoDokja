import test from "node:test";
import assert from "node:assert/strict";

import { baseName, proposalMatchesPerson, proposalPersons, viaLabelOf } from "../src/renderer/cards/proposals_model.js";
import type { KnowledgeProposal } from "../src/shared/replies.js";

function proposal(proposedBy: string, sentBy: string, tags: unknown): KnowledgeProposal {
  return {
    id: `p-${proposedBy}-${sentBy}`,
    kind: "web_article",
    payload: { url: "https://example.com/a", tags },
    proposedBy,
    sentBy,
    source: { channelId: "c1", messageId: "m1", permalink: "" },
    proposedAt: "2026-11-18T12:00:00Z",
  };
}

test("viaLabelOf names the search and the sent link, and nothing else", () => {
  assert.equal(viaLabelOf(["via:search"]), "via:search");
  assert.equal(viaLabelOf(["origin:discord", "via:link"]), "via:link");
  assert.equal(viaLabelOf(["origin:discord"]), "");
  assert.equal(viaLabelOf("via:search"), "");
  assert.equal(viaLabelOf(undefined), "");
});

test("baseName strips the verified account id from a person's name", () => {
  assert.equal(baseName("Tiago <1247657881155932170>"), "Tiago");
  assert.equal(baseName("Ningo"), "Ningo");
  assert.equal(baseName(""), "");
  assert.equal(baseName(42), "");
});

test("proposalPersons lists owner and Ningo first, everyone else sorted, each once", () => {
  const persons = proposalPersons([
    proposal("Ningo", "Tiago <1>", ["via:search"]),
    proposal("Sam <2>", "Sam <2>", ["via:link"]),
    proposal("Ningo", "owner", ["via:search"]),
    proposal("Iago <3>", "Iago <3>", ["via:link"]),
  ]);
  assert.deepEqual(persons, ["owner", "Ningo", "Iago", "Sam", "Tiago"]);
});

test("proposalMatchesPerson matches the proposer or whom the item was sent for", () => {
  const searchForTiago = proposal("Ningo", "Tiago <1>", ["via:search"]);
  assert.equal(proposalMatchesPerson(searchForTiago, "Ningo"), true);
  assert.equal(proposalMatchesPerson(searchForTiago, "Tiago"), true);
  assert.equal(proposalMatchesPerson(searchForTiago, "owner"), false);
});
