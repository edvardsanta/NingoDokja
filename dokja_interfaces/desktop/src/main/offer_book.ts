import { createHash } from "node:crypto";

// The store pages the orchestrator listed in a price search, kept in the shell. The screen gets
// an opaque token for each and never the address; opening one is the shell's, only for an http(s)
// address a price reply listed, on a real press. The newest pages are kept, the oldest forgotten.
const MAX_OFFERS = 2000;
const TOKEN = /^[0-9a-f]{16}$/;

const offers = new Map<string, string>();

function isWebAddress(value: string): boolean {
  try {
    const url = new URL(value);
    return (url.protocol === "https:" || url.protocol === "http:") && url.hostname !== "" && url.username === "" && url.password === "";
  } catch {
    return false;
  }
}

// Keeps a page and gives its token; an address that is not a web page gives "".
export function rememberOffer(address: string): string {
  const value = address.trim();
  if (!value || value.length > 500 || !isWebAddress(value)) return "";
  const token = createHash("sha256").update(value).digest("hex").slice(0, 16);
  offers.delete(token);
  offers.set(token, value);
  while (offers.size > MAX_OFFERS) {
    const oldest = offers.keys().next().value;
    if (oldest === undefined) break;
    offers.delete(oldest);
  }
  return token;
}

// The page behind a token, when a price reply listed it in this run.
export function offerAddress(token: unknown): string | undefined {
  return typeof token === "string" && TOKEN.test(token) ? offers.get(token) : undefined;
}

// For tests: forget every page.
export function forgetOffers(): void {
  offers.clear();
}
