import { createContext, useContext, useMemo, type ReactNode } from "react";

import type { Locale } from "../../shared/locale.js";
import { formatAgo, formatNumber } from "./format.js";
import { createTranslator, type Translate } from "./i18n.js";

const TranslateContext = createContext<Translate>(createTranslator("en"));
const LocaleContext = createContext<Locale>("en");

export function I18nProvider({ locale, children }: { locale: Locale; children: ReactNode }) {
  const translate = useMemo(() => createTranslator(locale), [locale]);
  return (
    <LocaleContext.Provider value={locale}>
      <TranslateContext.Provider value={translate}>{children}</TranslateContext.Provider>
    </LocaleContext.Provider>
  );
}

export function useTranslate(): Translate {
  return useContext(TranslateContext);
}

// Numbers formatted in the language of the screen.
export function useNumbers() {
  const locale = useContext(LocaleContext);
  return {
    fixed: (value: number, digits: number) => formatNumber(locale, value, digits),
    signed: (value: number, digits: number) => formatNumber(locale, value, digits, { signed: true }),
  };
}

// Times as the language of the screen says them.
export function useTimes() {
  const locale = useContext(LocaleContext);
  return { ago: (iso: string) => formatAgo(locale, iso) };
}
