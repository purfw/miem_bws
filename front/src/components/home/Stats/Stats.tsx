import styles from "./Stats.module.css";
import { dateLocale, pluralLabel } from "../../../i18n/runtime";

interface StatsProps {
  participants?: number;
  conferences?: number;
}

export function Stats({ participants = 0, conferences = 0 }: StatsProps) {
  const items = [
    { value: conferences.toLocaleString(dateLocale()), label: pluralLabel(conferences, ["конференция", "конференции", "конференций"], ["conference", "conferences"]) },
    { value: participants.toLocaleString(dateLocale()), label: pluralLabel(participants, ["участник", "участника", "участников"], ["participant", "participants"]) },
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
