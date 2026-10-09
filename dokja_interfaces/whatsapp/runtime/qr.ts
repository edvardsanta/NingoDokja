import qrcode from "qrcode-terminal";

// Draws the link code in the terminal. It is short-lived and only useful to someone holding the
// phone, but it still lets whoever scans it link the bot, so it goes to the log and nowhere else.
export function renderQr(qr: string, print: (text: string) => void = console.log): void {
  qrcode.generate(qr, { small: true }, (text: string) => print(text));
}
