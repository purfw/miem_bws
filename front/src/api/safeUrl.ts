import { tr } from "../i18n/runtime";

/** Returns the URL if it is https: or same-origin (relative or absolute); otherwise null. */
export function safeUrl(value: string | null | undefined): string | null {
  if (!value) return null;
  let url: URL;
  try {
    url = new URL(value, window.location.origin);
  } catch {
    return null;
  }
  if (url.origin === window.location.origin && (url.protocol === "https:" || url.protocol === "http:")) return url.href;
  return url.protocol === "https:" ? url.href : null;
}

/** Validates a server-provided URL before navigation; throws a readable error for unsafe schemes/origins. */
export function safeRedirectUrl(value: string | null | undefined): string {
  const url = safeUrl(value);
  if (!url) throw new Error(tr("Сервер вернул недопустимую ссылку.", "The server returned an invalid link."));
  return url;
}
