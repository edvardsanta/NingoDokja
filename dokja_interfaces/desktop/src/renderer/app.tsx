import { useEffect } from "react";

import type { Locale } from "../shared/locale.js";
import type { Transport } from "../shared/transport.js";
import { CARDS, START } from "./cards/registry.js";
import { I18nProvider } from "./i18n/context.js";
import { PulseProvider, usePulse } from "./pulse.js";
import { RuntimeEnabled } from "./runtime_context.js";
import { Sidebar } from "./sidebar.js";

function Wordmark() {
  const { pulse } = usePulse();
  return (
    <h1 className="wordmark" data-pulse={pulse}>
      Ningo
    </h1>
  );
}

export function App({ transport, locale, runtimeControls=false }: { transport: Transport; locale: Locale; runtimeControls?:boolean }) {
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  return (
    <I18nProvider locale={locale}>
      <RuntimeEnabled.Provider value={runtimeControls}><PulseProvider>
        <main>
          <Sidebar entries={CARDS} transport={transport} start={START} header={<Wordmark />} />
        </main>
      </PulseProvider></RuntimeEnabled.Provider>
    </I18nProvider>
  );
}
