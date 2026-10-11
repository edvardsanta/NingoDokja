import { encode, renderUnicodeCompact } from "uqr";

// Draws the link code in the terminal. It is short-lived and only useful to someone holding the
// phone, but it still lets whoever scans it link the bot, so it goes to the log and nowhere else.
export function renderQr(qr: string, print: (text: string) => void = console.log): void {
  print(renderUnicodeCompact(qr, { border: 1 }));
}

// The same code as rows of "0" and "1" (one character per module, no quiet zone), for the screen
// that draws it. It is data, not markup, so whoever draws it builds the picture itself.
export function qrRows(qr: string): string[] {
  return encode(qr, { border: 0 }).data.map((row) => row.map((dark) => (dark ? "1" : "0")).join(""));
}
