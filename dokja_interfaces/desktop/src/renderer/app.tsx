import { useEffect } from "react";

import type { Locale } from "../shared/locale.js";
import type { Transport } from "../shared/transport.js";
import { CARDS } from "./cards/registry.js";
import { I18nProvider } from "./i18n/context.js";
import { PulseProvider, usePulse } from "./pulse.js";
import { Tabs } from "./tabs.js";

function Wordmark() {
  const { pulse } = usePulse();
  return (
    <h1 className="wordmark" data-pulse={pulse}>
      Ningo
    </h1>
  );
}

export function App({ transport, locale }: { transport: Transport; locale: Locale }) {
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  return (
    <I18nProvider locale={locale}>
      <PulseProvider>
        <main>
          <Wordmark />
          <Tabs entries={CARDS} transport={transport} />
        </main>
      </PulseProvider>
    </I18nProvider>
  );
}
