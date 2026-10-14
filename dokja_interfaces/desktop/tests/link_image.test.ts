import test from "node:test";
import assert from "node:assert/strict";

import { linkImage } from "../src/main/link_image.js";

function grid(side: number, dark: (x: number, y: number) => boolean): string[] {
  return Array.from({ length: side }, (_, y) => Array.from({ length: side }, (_, x) => (dark(x, y) ? "1" : "0")).join(""));
}

const decode = (url: string) => Buffer.from(url.replace("data:image/svg+xml;base64,", ""), "base64").toString("utf8");

test("a grid of modules becomes an SVG data URL with a quiet zone, one rectangle per dark run", () => {
  const rows = grid(21, (x, y) => (x + y) % 2 === 0 && x < 4);

  const url = linkImage(rows);

  assert.ok(url?.startsWith("data:image/svg+xml;base64,"));
  const svg = decode(url!);
  assert.match(svg, /viewBox="0 0 29 29"/);
  assert.match(svg, /^<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg"/);
  // Every dark module is drawn at its place shifted by the quiet zone of 4.
  assert.match(svg, /M4 4h1v1h-1z/);
  assert.equal((svg.match(/M\d+ \d+h\d+v1h-\d+z/g) ?? []).length, 21 * 2);
});

test("a long dark run is one rectangle", () => {
  const url = linkImage(grid(21, (_x, y) => y === 0));

  assert.match(decode(url!), /M4 4h21v1h-21z/);
});

test("nothing that is not a square grid of 0 and 1 within QR sizes becomes a picture", () => {
  const good = grid(25, () => false);
  const cases: Record<string, unknown> = {
    "not a list": "0101",
    "null": null,
    "too small": grid(20, () => false),
    "too large": grid(178, () => false),
    "not square": [...good.slice(0, 24), "0101"],
    "markup": [...good.slice(0, 24), `<svg onload=alert(1)>${"0".repeat(10)}`],
    "other digits": [...good.slice(0, 24), "2".repeat(25)],
    "not strings": [...good.slice(0, 24), 12345],
  };
  for (const [name, value] of Object.entries(cases)) {
    assert.equal(linkImage(value), undefined, name);
  }
});

test("the largest QR code is accepted", () => {
  assert.ok(linkImage(grid(177, (x, y) => (x * y) % 3 === 0)));
});
