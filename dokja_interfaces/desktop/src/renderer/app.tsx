import { useEffect } from "react";

import type { Locale } from "../shared/locale.js";
import type { Transport } from "../shared/transport.js";
import { CARDS, FALLBACK, START } from "./cards/registry.js";
import { I18nProvider } from "./i18n/context.js";
import { RuntimeEnabled } from "./runtime_context.js";
import { Sidebar } from "./sidebar.js";
import { StatusProvider, useStatus } from "./status_context.js";

function Wordmark() {
  const { pulse } = useStatus();
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
      <RuntimeEnabled.Provider value={runtimeControls}><StatusProvider transport={transport}>
        <main>
          <Sidebar entries={CARDS} transport={transport} start={START} fallback={FALLBACK} header={<Wordmark />} />
        </main>
      </StatusProvider></RuntimeEnabled.Provider>
    </I18nProvider>
  );
}
