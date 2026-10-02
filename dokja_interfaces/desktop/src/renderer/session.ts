import { localeFromTag, type Locale } from "../shared/locale.js";
import type { Transport } from "../shared/transport.js";

export type Session = { transport: Transport; locale: Locale };

const noShell: Transport = {
  async request() {
    return {
      ok: false,
      error: { code: "unavailable", message: "the page is not running inside the app" },
    };
  },
};

// Inside the app the shell supplies the transport and the language. In a plain browser there is no
// shell: development gets a fake with made-up data, anything else gets a transport that says so.
export async function loadSession(): Promise<Session> {
  if (window.dokja) {
    const bootstrap = await window.dokja.bootstrap();
    return { transport: window.dokja, locale: bootstrap?.locale ?? "en" };
  }

  const locale = localeFromTag(navigator.language);
  if (import.meta.env.DEV) {
    const { createFakeTransport } = await import("./fake_transport.js");
    return { transport: createFakeTransport(), locale };
  }
  return { transport: noShell, locale };
}
