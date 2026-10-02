import { createContext, useContext, useMemo, type ReactNode } from "react";

import type { Locale } from "../../shared/locale.js";
import { createTranslator, type Translate } from "./i18n.js";

const TranslateContext = createContext<Translate>(createTranslator("en"));

export function I18nProvider({ locale, children }: { locale: Locale; children: ReactNode }) {
  const translate = useMemo(() => createTranslator(locale), [locale]);
  return <TranslateContext.Provider value={translate}>{children}</TranslateContext.Provider>;
}

export function useTranslate(): Translate {
  return useContext(TranslateContext);
}
