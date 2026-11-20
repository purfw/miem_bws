import { useEffect, useState } from "react";
import { useAuth } from "../../auth/useAuth";
import type { RecommendationsResponse } from "../../types/conference";
import { reasonLabel } from "./reasons";
import { ConferenceCard } from "../home/ConferenceCard/ConferenceCard";
import styles from "./Recommendations.module.css";

interface RecommendationsProps {
  /** API path: /home/recommendations or /conferences/{id}/similar. */
  source: string;
  limit?: number;
  /** Title for a personalised list (or for a list that is always specific, such as similar conferences). */
  title: string;
  /** Title for the cold-start list of popular conferences. */
  fallbackTitle?: string;
  eyebrow: string;
}

export function Recommendations({ source, limit = 3, title, fallbackTitle, eyebrow }: RecommendationsProps) {
  const { request, user } = useAuth();
  const [result, setResult] = useState<{ key: string; response: RecommendationsResponse | null }>({ key: "", response: null });
  // Personal recommendations depend on who is signed in.
  const key = `${source}?limit=${limit}|${user?.id ?? ""}`;

  useEffect(() => {
    const controller = new AbortController();
    request<RecommendationsResponse>(`${source}?limit=${limit}`, { signal: controller.signal })
      .then((response) => { if (!controller.signal.aborted) setResult({ key, response }); })
      // Recommendations are optional: a failure hides the block instead of breaking the page.
      .catch(() => { if (!controller.signal.aborted) setResult({ key, response: null }); });
    return () => controller.abort();
  }, [request, source, limit, key]);

  const response = result.key === key ? result.response : null;
  if (!response || response.data.length === 0) return null;
  const heading = response.personalised || !fallbackTitle ? title : fallbackTitle;

  return (
    <section className={styles.section} aria-labelledby={`rec-${source}`}>
      <span className={styles.eyebrow}>{eyebrow}</span>
      <h2 className={styles.title} id={`rec-${source}`}>{heading}</h2>
      <div className={styles.grid}>
        {response.data.map((item) => (
          <ConferenceCard key={item.id} conference={item} reasons={item.reasons.map(reasonLabel)} />
        ))}
      </div>
    </section>
  );
}
