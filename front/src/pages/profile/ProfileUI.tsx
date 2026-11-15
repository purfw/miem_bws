import type { ReactNode } from "react";
import { Button } from "../../components/ui/Button/Button";
import { ButtonLink } from "../../components/ui/Button/ButtonLink";
import { tr } from "../../i18n/runtime";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

export function ProfileSkeleton() {
  return (
    <div className={styles.list} role="status" aria-busy="true">
      <span className={styles.visuallyHidden}>
        {tr("Загрузка…", "Loading…")}
      </span>
      {[0, 1, 2].map((item) => (
        <div
          key={item}
          className={`${styles.panel} ${styles.skeleton}`}
          aria-hidden="true"
        >
          <span className={styles.skeletonLine} />
          <span className={styles.skeletonLine} />
          <span className={styles.skeletonLine} />
        </div>
      ))}
    </div>
  );
}
export function ProfileLoadError({
  error,
  retry,
}: {
  error: string;
  retry: () => void;
}) {
  return (
    <div className={`${styles.panel} ${styles.stack}`}>
      <p className={page.error} role="alert">
        {error}
      </p>
      <div>
        <Button variant="secondary" onClick={retry}>
          {tr("Повторить", "Retry")}
        </Button>
      </div>
    </div>
  );
}
export function ProfileEmpty({ children }: { children: ReactNode }) {
  return (
    <div className={`${styles.panel} ${styles.empty}`}>
      <p className={styles.muted}>{children}</p>
      <ButtonLink variant="primary" to="/conferences">
        {tr("Найти мероприятие", "Find an event")}
      </ButtonLink>
    </div>
  );
}
export function ProfileBadge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "active" | "accepted";
}) {
  return (
    <span
      className={`${styles.badge} ${tone === "active" ? styles.badgeActive : tone === "accepted" ? styles.badgeAccepted : ""}`}
    >
      {children}
    </span>
  );
}
export function ProfileFeedback({
  error,
  message,
}: {
  error?: string;
  message?: string;
}) {
  return (
    <>
      {error && (
        <p className={page.error} role="alert">
          {error}
        </p>
      )}
      {message && (
        <p className={page.notice} role="status">
          {message}
        </p>
      )}
    </>
  );
}
