export type Locale = "ru" | "en";

export const localeStorageKey = "confhub-locale";

export function initialLocale(): Locale {
  if (typeof window === "undefined") return "ru";
  const saved = window.localStorage.getItem(localeStorageKey);
  if (saved === "ru" || saved === "en") return saved;
  return window.navigator.language.toLowerCase().startsWith("ru") ? "ru" : "en";
}

let activeLocale: Locale = initialLocale();

export function activateLocale(locale: Locale) {
  activeLocale = locale;
}

export function tr(ru: string, en: string): string {
  return activeLocale === "en" ? en : ru;
}

export function pluralLabel(count: number, ru: [string, string, string], en: [string, string]): string {
  if (activeLocale === "en") return en[count === 1 ? 0 : 1];
  const form = new Intl.PluralRules("ru").select(count);
  return ru[form === "one" ? 0 : form === "few" ? 1 : 2];
}

export function localize(ru: string, en?: string | null): string {
  return activeLocale === "en" && en?.trim() ? en : ru;
}

export function dateLocale(): string {
  return activeLocale === "en" ? "en-GB" : "ru-RU";
}
