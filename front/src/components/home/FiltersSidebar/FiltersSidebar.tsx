import { useState } from "react";
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
  const fallbackTopics: FilterOption[] = [
    ["Нейронауки", "Neuroscience"], ["Машинное обучение", "Machine learning"],
    ["Биоинформатика", "Bioinformatics"], ["Лингвистика", "Linguistics"],
    ["Физика", "Physics"], ["Археология", "Archaeology"],
  ].map(([itemValue, english]) => ({ value: itemValue, label: tr(itemValue, english), count: 0, selected: false }));
  const fallbackFormats: FilterOption[] = [
    { value: "offline", label: tr("Очно", "In person"), count: 0, selected: false },
    { value: "hybrid", label: tr("Гибрид", "Hybrid"), count: 0, selected: false },
    { value: "online", label: tr("Онлайн", "Online"), count: 0, selected: false },
  ];
  const [topicsExpanded, setTopicsExpanded] = useState(false);
  const topics = facetValues(facets, "topics", fallbackTopics);
  const formats = facetValues(facets, "format", fallbackFormats);
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
                <span>{facet.value === "offline" ? tr("Очно", "In person") : facet.value === "hybrid" ? tr("Гибрид", "Hybrid") : facet.value === "online" ? tr("Онлайн", "Online") : facet.label}</span>
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
        <input id="catalog-city" type="search" className={styles.cityInput} placeholder={tr("Любой", "Any")} value={value.city} onChange={(event) => onChange({ ...value, city: event.target.value })} />
      </section>
    </aside>
  );
}
