import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { readableError } from "../../../api/client";
import { useAuth } from "../../../auth/useAuth";
import type { ConferenceListResponse } from "../../../types/conference";
import { ConferenceCard } from "../../home/ConferenceCard/ConferenceCard";
import { FiltersSidebar, type FilterState } from "../../home/FiltersSidebar/FiltersSidebar";
import { Pagination } from "../Pagination/Pagination";
import styles from "./CatalogView.module.css";
import { tr } from "../../../i18n/runtime";

const TABS = ["all", "active", "upcoming", "completed"] as const;
type Tab = (typeof TABS)[number];
const emptyFilters: FilterState = { topics: [], formats: [], from: "", to: "", city: "" };

function parseTab(value: string | null): Tab {
  return TABS.find((item) => item === value) ?? "all";
}

function list(value: string | null): string[] {
  return value ? value.split(",").filter(Boolean) : [];
}

export function CatalogView() {
  const tabs: Array<{ id: Tab | "mine"; label: string }> = [
    { id: "all", label: tr("Все", "All") }, { id: "active", label: tr("Активные", "Active") },
    { id: "upcoming", label: tr("Скоро", "Upcoming") }, { id: "completed", label: tr("Завершены", "Completed") },
    { id: "mine", label: tr("Мои", "Mine") },
  ];
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const { request } = useAuth();
  const [result, setResult] = useState<ConferenceListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const searchString = params.toString();
  const tab = parseTab(params.get("tab"));
  const page = Math.max(1, Number(params.get("page")) || 1);
  const sort = params.get("sort") ?? "start_date";
  const filters: FilterState = {
    topics: list(params.get("topics")),
    formats: list(params.get("format")),
    from: params.get("from_date") ?? "",
    to: params.get("to_date") ?? "",
    city: params.get("city") ?? "",
  };

  useEffect(() => {
    const controller = new AbortController();
    let clamped = false;
    const query = new URLSearchParams(searchString);
    query.set("page", String(page));
    query.set("per_page", "9");
    if (tab === "all") {
      query.delete("tab");
      query.set("status", "registration_open,registration_closed,in_progress,completed,archived");
    } else {
      query.delete("status");
      query.set("tab", tab === "completed" ? "past" : tab);
    }
    request<ConferenceListResponse>(`/conferences?${query.toString()}`, { signal: controller.signal })
      .then((response) => {
        if (controller.signal.aborted) return;
        const lastPage = response.pagination.total_pages;
        if (lastPage >= 1 && page > lastPage) {
          // Out-of-range page in the URL: clamp it; the effect reruns and loads the last page.
          clamped = true;
          setParams((current) => {
            const next = new URLSearchParams(current);
            if (lastPage === 1) next.delete("page");
            else next.set("page", String(lastPage));
            return next;
          }, { replace: true });
          return;
        }
        setResult(response);
      })
      .catch((reason: unknown) => {
        if (reason instanceof DOMException && reason.name === "AbortError") return;
        setError(readableError(reason));
      })
      .finally(() => { if (!controller.signal.aborted && !clamped) setLoading(false); });
    return () => controller.abort();
  }, [request, searchString, page, tab, retry, setParams]);

  function mutate(changes: Record<string, string | null>) {
    const next = new URLSearchParams(params);
    Object.entries(changes).forEach(([key, value]) => {
      if (value) next.set(key, value);
      else next.delete(key);
    });
    if (next.toString() === params.toString()) return;
    setLoading(true);
    setError("");
    setParams(next);
  }

  function setFilters(value: FilterState) {
    mutate({
      topics: value.topics.join(",") || null,
      format: value.formats.join(",") || null,
      from_date: value.from || null,
      to_date: value.to || null,
      city: value.city.trim() || null,
      page: null,
    });
  }

  const total = result?.pagination.total ?? 0;
  const totalPages = result?.pagination.total_pages ?? 0;

  return (
    <section className={styles.catalog}>
      <header className={styles.header}>
        <div className={styles.heading}>
          <span className={styles.eyebrow}>{tr("Каталог", "Catalogue")} · {loading ? tr("загрузка", "loading") : `${total} ${tr("результатов", "results")}`}</span>
          <h1 className={styles.title}>{tr("Каталог конференций", "Conference catalogue")}</h1>
        </div>

        <div className={styles.controls}>
          <div className={styles.tabs} role="tablist" aria-label={tr("Состояние конференций", "Conference status")}>
            {tabs.map((item) => (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={item.id !== "mine" && tab === item.id}
                className={`${styles.tab} ${item.id !== "mine" && tab === item.id ? styles.tabActive : ""}`}
                onClick={() => item.id === "mine" ? navigate("/profile") : mutate({ tab: item.id === "all" ? null : item.id, page: null })}
              >
                {item.label}
              </button>
            ))}
          </div>

          <label>
            <span className={styles.visuallyHidden}>{tr("Сортировка", "Sort")}</span>
            <select className={styles.sort} value={sort} onChange={(event) => mutate({ sort: event.target.value, page: null })}>
              <option value="start_date">{tr("Сначала ближайшие", "Nearest first")}</option>
              <option value="-start_date">{tr("Сначала поздние", "Latest first")}</option>
              <option value="-created_at">{tr("Сначала новые", "Newest first")}</option>
              <option value="title">{tr("По названию", "By title")}</option>
            </select>
          </label>
        </div>
      </header>

      <div className={styles.layout}>
        <FiltersSidebar
          value={filters}
          onChange={setFilters}
          onReset={() => setFilters(emptyFilters)}
          facets={result?.filters}
          showPeriod
          expandableTopics
        />

        <div className={styles.content} aria-live="polite" aria-busy={loading}>
          {error ? (
            <div className={styles.empty}><p>{tr("Не удалось загрузить каталог", "Could not load the catalogue")}</p><span>{error}</span><button type="button" className={styles.retry} onClick={() => { setLoading(true); setError(""); setRetry((value) => value + 1); }}>{tr("Повторить", "Retry")}</button></div>
          ) : loading ? (
            <div className={styles.empty}><p>{tr("Загружаем конференции…", "Loading conferences…")}</p></div>
          ) : result && result.data.length > 0 ? (
            <div className={styles.grid}>
              {result.data.map((conference) => <ConferenceCard key={conference.id} conference={conference} />)}
            </div>
          ) : (
            <div className={styles.empty}><p>{tr("Ничего не нашлось.", "No results found.")}</p><span>{tr("Попробуйте изменить фильтры или поисковый запрос.", "Try changing the filters or search query.")}</span></div>
          )}

          {!loading && !error && total > 0 && (
            <footer className={styles.footer}>
              <span className={styles.shown}>{tr("Показано", "Showing")} {result?.data.length ?? 0} {tr("из", "of")} {total}</span>
              {totalPages > 1 && <Pagination current={page} total={totalPages} onChange={(next) => mutate({ page: String(next) })} />}
            </footer>
          )}
        </div>
      </div>
    </section>
  );
}
