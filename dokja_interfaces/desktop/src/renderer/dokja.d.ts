import type { Bootstrap, Transport } from "../shared/transport.js";

declare global {
  interface Window {
    // Present only inside the Electron shell (see src/preload/preload.ts).
    dokja?: Transport & { bootstrap(): Promise<Bootstrap | null> };
  }
}

export {};
