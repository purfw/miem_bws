import { useEffect, useMemo, useState, type ReactNode } from "react";
import { LocaleContext } from "./locale-context";
import { activateLocale, initialLocale, localeStorageKey, type Locale } from "./runtime";

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, updateLocale] = useState<Locale>(() => initialLocale());

  useEffect(() => {
    activateLocale(locale);
    document.documentElement.lang = locale;
    try {
      window.localStorage.setItem(localeStorageKey, locale);
    } catch {
      // Storage may be blocked or full; the locale still applies for this session.
    }
  }, [locale]);

  const value = useMemo(() => ({
    locale,
    setLocale: (next: Locale) => {
      activateLocale(next);
      updateLocale(next);
    },
  }), [locale]);

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}
