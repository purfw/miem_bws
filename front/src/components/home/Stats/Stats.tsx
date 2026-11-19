import styles from "./Stats.module.css";
import { dateLocale, pluralLabel } from "../../../i18n/runtime";

interface StatsProps {
  participants?: number;
  conferences?: number;
}

// Undefined means "not loaded yet / failed": show a placeholder instead of a misleading 0.
function statValue(count?: number): string {
  return count === undefined ? "—" : count.toLocaleString(dateLocale());
}

export function Stats({ participants, conferences }: StatsProps) {
  const items = [
    { value: statValue(conferences), label: pluralLabel(conferences ?? 0, ["конференция", "конференции", "конференций"], ["conference", "conferences"]) },
    { value: statValue(participants), label: pluralLabel(participants ?? 0, ["участник", "участника", "участников"], ["participant", "participants"]) },
  ];
  return (
    <dl className={styles.stats}>
      {items.map((item) => (
        <div key={item.label} className={styles.item}>
          <dt className={styles.value}>{item.value}</dt>
          <dd className={styles.label}>{item.label}</dd>
        </div>
      ))}
    </dl>
  );
}
