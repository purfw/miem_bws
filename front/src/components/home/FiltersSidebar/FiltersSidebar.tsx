import { useEffect, useRef, useState } from "react";
import type { CatalogFilter, FilterOption } from "../../../types/conference";
import styles from "./FiltersSidebar.module.css";
import { tr } from "../../../i18n/runtime";

export interface FilterState {
  topics: string[];
  formats: string[];
  from: string;
  to: string;
  city: string;
}

interface FiltersSidebarProps {
  value: FilterState;
  onChange: (value: FilterState) => void;
  onReset: () => void;
  facets?: CatalogFilter[];
  showPeriod?: boolean;
  expandableTopics?: boolean;
}

const COLLAPSED_TOPICS = 4;

function facetValues(facets: CatalogFilter[] | undefined, key: string, fallback: FilterOption[]) {
  return facets?.find((item) => item.key === key)?.values ?? fallback;
}

export function FiltersSidebar({
  value,
  onChange,
  onReset,
  facets,
  showPeriod = false,
  expandableTopics = false,
}: FiltersSidebarProps) {
  const fallbackFormats: FilterOption[] = [
    { value: "offline", label: tr("Очно", "In person"), count: 0, selected: false },
    { value: "hybrid", label: tr("Гибрид", "Hybrid"), count: 0, selected: false },
    { value: "online", label: tr("Онлайн", "Online"), count: 0, selected: false },
  ];
  const [topicsExpanded, setTopicsExpanded] = useState(false);
  // Topics come only from API facets: no placeholder topics with fake zero counts.
  const topics = facetValues(facets, "topics", []);
  const formats = facetValues(facets, "format", fallbackFormats);
  const cities = facetValues(facets, "city_options", []);
  // The city field is applied after a short pause so typing does not reload
  // the catalogue on every keystroke. An external change (reset) replaces the draft.
  const [city, setCity] = useState({ source: value.city, draft: value.city });
  const cityDraft = city.source === value.city ? city.draft : value.city;
  const latest = useRef({ value, onChange });
  useEffect(() => { latest.current = { value, onChange }; });
  useEffect(() => {
    if (cityDraft.trim() === value.city.trim()) return;
    const timer = window.setTimeout(() => latest.current.onChange({ ...latest.current.value, city: cityDraft }), 400);
    return () => window.clearTimeout(timer);
  }, [cityDraft, value.city]);
  const visibleTopics = expandableTopics && !topicsExpanded ? topics.slice(0, COLLAPSED_TOPICS) : topics;

  function toggle(key: "topics" | "formats", item: string) {
    const selected = value[key];
    onChange({
      ...value,
      [key]: selected.includes(item) ? selected.filter((entry) => entry !== item) : [...selected, item],
    });
  }

  return (
    <aside className={styles.sidebar} aria-label={tr("Фильтры каталога", "Catalogue filters")}>
      <header className={styles.header}>
        <span className={styles.title}>{tr("Фильтры", "Filters")}</span>
        <button type="button" className={styles.reset} onClick={onReset}>{tr("Сбросить", "Reset")}</button>
      </header>

      <section className={styles.group}>
        <h4 className={styles.groupTitle}>{tr("ТЕМАТИКА", "TOPICS")}</h4>
        <ul className={styles.list}>
          {visibleTopics.map((facet) => (
            <li key={facet.value} className={styles.row}>
              <label className={styles.label}>
                <input
                  type="checkbox"
                  className={styles.checkbox}
                  checked={value.topics.includes(facet.value)}
                  onChange={() => toggle("topics", facet.value)}
                />
                <span>{facet.label}</span>
              </label>
              <span className={styles.count}>{facet.count}</span>
            </li>
          ))}
        </ul>
        {expandableTopics && topics.length > COLLAPSED_TOPICS && (
          <button type="button" className={styles.showAll} onClick={() => setTopicsExpanded((item) => !item)}>
            {topicsExpanded ? tr("− Свернуть", "− Collapse") : tr("+ Показать все", "+ Show all")}
          </button>
        )}
      </section>

      {showPeriod && (
        <>
          <hr className={styles.divider} />
          <section className={styles.group}>
            <h4 className={styles.groupTitle}>{tr("ПЕРИОД", "PERIOD")}</h4>
            <div className={styles.period}>
              <label>
                <span className={styles.visuallyHidden}>{tr("Дата с", "Date from")}</span>
                <input type="date" className={styles.periodInput} value={value.from} onChange={(event) => onChange({ ...value, from: event.target.value })} />
              </label>
              <label>
                <span className={styles.visuallyHidden}>{tr("Дата по", "Date to")}</span>
                <input type="date" className={styles.periodInput} value={value.to} onChange={(event) => onChange({ ...value, to: event.target.value })} />
              </label>
            </div>
          </section>
        </>
      )}

      <hr className={styles.divider} />
      <section className={styles.group}>
        <h4 className={styles.groupTitle}>{tr("ФОРМАТ", "FORMAT")}</h4>
        <ul className={styles.list}>
          {formats.map((facet) => (
            <li key={facet.value} className={styles.row}>
              <label className={styles.label}>
                <input type="checkbox" className={styles.checkbox} checked={value.formats.includes(facet.value)} onChange={() => toggle("formats", facet.value)} />
                <span>{facet.value === "offline" ? tr("Очно", "In person") : facet.value === "hybrid" ? tr("Гибрид", "Hybrid") : facet.value === "online" ? tr("Онлайн", "Online") : facet.label}</span>
              </label>
            </li>
          ))}
        </ul>
      </section>

      <hr className={styles.divider} />
      <section className={styles.group}>
        <label className={styles.groupTitle} htmlFor="catalog-city">{tr("ГОРОД", "CITY")}</label>
        <input id="catalog-city" type="search" className={styles.cityInput} placeholder={tr("Любой", "Any")} list="catalog-city-options" autoComplete="off" value={cityDraft} onChange={(event) => setCity({ source: value.city, draft: event.target.value })} />
        <datalist id="catalog-city-options">
          {cities.map((item) => <option key={item.value} value={item.value}>{`${item.label} (${item.count})`}</option>)}
        </datalist>
      </section>
    </aside>
  );
}
