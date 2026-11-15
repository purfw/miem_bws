import styles from "./Pagination.module.css";
import { tr } from "../../../i18n/runtime";

interface PaginationProps {
  current: number;
  total: number;
  onChange: (page: number) => void;
}

const ELLIPSIS = "ellipsis" as const;
type PageItem = number | typeof ELLIPSIS;

function buildPages(current: number, total: number): PageItem[] {
  if (total <= 5) {
    return Array.from({ length: total }, (_, i) => i + 1);
  }

  const pages = new Set<number>([1, 2, 3, current, total]);
  const sorted = [...pages].filter((p) => p >= 1 && p <= total).sort((a, b) => a - b);

  const result: PageItem[] = [];
  let previous = 0;
  for (const page of sorted) {
    if (page - previous > 1) {
      result.push(ELLIPSIS);
    }
    result.push(page);
    previous = page;
  }
  return result;
}

export function Pagination({ current, total, onChange }: PaginationProps) {
  const pages = buildPages(current, total);

  return (
    <nav className={styles.pagination} aria-label={tr("Страницы", "Pages")}>
      <button
        type="button"
        className={styles.arrow}
        onClick={() => onChange(current - 1)}
        disabled={current === 1}
        aria-label={tr("Назад", "Previous")}
      >
        ←
      </button>

      {pages.map((page, index) =>
        page === ELLIPSIS ? (
          <span key={`ellipsis-${index}`} className={styles.ellipsis}>
            …
          </span>
        ) : (
          <button
            key={page}
            type="button"
            className={`${styles.page} ${page === current ? styles.pageActive : ""}`}
            onClick={() => onChange(page)}
            aria-current={page === current ? "page" : undefined}
          >
            {page}
          </button>
        )
      )}

      <button
        type="button"
        className={styles.arrow}
        onClick={() => onChange(current + 1)}
        disabled={current === total}
        aria-label={tr("Вперёд", "Next")}
      >
        →
      </button>
    </nav>
  );
}
