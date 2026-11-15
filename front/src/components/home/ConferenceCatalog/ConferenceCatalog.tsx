import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { readableError } from "../../../api/client";
import { useAuth } from "../../../auth/useAuth";
import type { ConferenceListResponse } from "../../../types/conference";
import { ConferenceCard } from "../ConferenceCard/ConferenceCard";
import { FiltersSidebar, type FilterState } from "../FiltersSidebar/FiltersSidebar";
import styles from "./ConferenceCatalog.module.css";
import { tr } from "../../../i18n/runtime";

type Tab = "active" | "upcoming" | "past";
const emptyFilters: FilterState = { topics: [], formats: [], from: "", to: "", city: "" };

export function ConferenceCatalog() {
  const tabs: Array<{ id: Tab; label: string }> = [
    { id: "active", label: tr("Активные", "Active") }, { id: "upcoming", label: tr("Скоро", "Upcoming") },
    { id: "past", label: tr("Завершены", "Completed") },
  ];
  const { request } = useAuth();
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<Tab>("active");
  const [filters, setFilters] = useState<FilterState>(emptyFilters);
  const [result, setResult] = useState<ConferenceListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    const query = new URLSearchParams({ tab: activeTab, per_page: "6", sort: "start_date" });
    if (filters.topics.length) query.set("topics", filters.topics.join(","));
    if (filters.formats.length) query.set("format", filters.formats.join(","));
    if (filters.city.trim()) query.set("city", filters.city.trim());
    request<ConferenceListResponse>(`/conferences?${query.toString()}`, { signal: controller.signal })
      .then(setResult)
      .catch((reason: unknown) => {
        if (reason instanceof DOMException && reason.name === "AbortError") return;
        setError(readableError(reason));
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [activeTab, filters, request]);

  function changeFilters(value: FilterState) {
    if (JSON.stringify(value) === JSON.stringify(filters)) return;
    setLoading(true);
    setError("");
    setFilters(value);
  }

  return (
    <section className={styles.catalog}>
      <header className={styles.header}>
        <div>
          <span className={styles.eyebrow}>{tr("КАТАЛОГ", "CATALOGUE")}</span>
          <h2 className={styles.title}>{tr("Список конференций", "Conference list")}</h2>
        </div>
        <div className={styles.tabs} role="tablist" aria-label={tr("Состояние конференций", "Conference status")}>
          {tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={activeTab === tab.id}
              className={`${styles.tab} ${activeTab === tab.id ? styles.tabActive : ""}`}
              onClick={() => { if (activeTab === tab.id) return; setLoading(true); setError(""); setActiveTab(tab.id); }}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </header>

      <div className={styles.layout}>
        <FiltersSidebar value={filters} onChange={changeFilters} onReset={() => changeFilters(emptyFilters)} facets={result?.filters} />
        <div className={styles.results} aria-live="polite" aria-busy={loading}>
          {loading ? (
            <p className={styles.message}>{tr("Загружаем конференции…", "Loading conferences…")}</p>
          ) : error ? (
            <p className={styles.message}>{error}</p>
          ) : result && result.data.length > 0 ? (
            <div className={styles.grid}>
              {result.data.map((conference) => <ConferenceCard key={conference.id} conference={conference} />)}
            </div>
          ) : (
            <p className={styles.message}>{tr("По выбранным условиям конференций пока нет.", "No conferences match the selected filters yet.")}</p>
          )}
          {!loading && !error && (result?.pagination.total ?? 0) > (result?.data.length ?? 0) && (
            <button type="button" className={styles.more} onClick={() => navigate(`/conferences?tab=${activeTab === "past" ? "completed" : activeTab}`)}>
              {tr("Смотреть весь каталог", "View full catalogue")}
            </button>
          )}
        </div>
      </div>
    </section>
  );
}
