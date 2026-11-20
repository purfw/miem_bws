import type { CoAuthor } from "../../types/conference";

export const MAX_CO_AUTHORS = 20;

/** Trims fields and drops empty optional values before sending. */
export function cleanCoAuthors(value: CoAuthor[]): CoAuthor[] {
  return value
    .map((item) => ({ name: item.name.trim(), email: item.email?.trim() || undefined, affiliation: item.affiliation?.trim() || undefined }))
    .filter((item) => item.name);
}
