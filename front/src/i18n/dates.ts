import { dateLocale } from "./runtime";

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Parses "YYYY-MM-DD" as a local calendar date (new Date() would treat it as UTC midnight); other values as-is. */
export function parseDate(value: string): Date {
  const match = DATE_ONLY.exec(value);
  return match ? new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : new Date(value);
}

/** Today's date in the user's local time zone as "YYYY-MM-DD". */
export function todayLocalISO(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

/** "3–5 Feb 2027"; includes the month (and year) on both ends when they differ: "30 Jan – 2 Feb 2027". */
export function formatDayRange(start: string, end: string, month: "short" | "long"): string {
  const from = parseDate(start);
  const to = parseDate(end);
  const locale = dateLocale();
  if (from.getFullYear() !== to.getFullYear()) {
    const options = { day: "numeric", month, year: "numeric" } as const;
    return `${from.toLocaleDateString(locale, options)} – ${to.toLocaleDateString(locale, options)}`;
  }
  if (from.getMonth() !== to.getMonth()) {
    const options = { day: "numeric", month } as const;
    return `${from.toLocaleDateString(locale, options)} – ${to.toLocaleDateString(locale, options)} ${to.getFullYear()}`;
  }
  // day+month formatting yields the genitive in Russian ("5 февраля", not "февраль").
  const endDay = to.toLocaleDateString(locale, { day: "numeric", month });
  return `${from.getDate() === to.getDate() ? "" : `${from.getDate()}–`}${endDay} ${to.getFullYear()}`;
}
