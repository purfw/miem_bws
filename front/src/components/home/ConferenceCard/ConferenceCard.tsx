import { useNavigate } from "react-router-dom";
import type { Conference } from "../../../types/conference";
import { Button } from "../../ui/Button/Button";
import styles from "./ConferenceCard.module.css";
import calendarIcon from "../../../assets/icons/calendar.svg";
import locationIcon from "../../../assets/icons/location.svg";
import { dateLocale, localize, pluralLabel, tr } from "../../../i18n/runtime";

interface ConferenceCardProps {
  conference: Conference;
}

function statusLabel(status: Conference["status"]): string {
  const labels: Record<Conference["status"], [string, string]> = {
    draft: ["Черновик", "Draft"], registration_open: ["Регистрация открыта", "Registration open"],
    registration_closed: ["Регистрация закрыта", "Registration closed"], in_progress: ["Идёт сейчас", "In progress"],
    completed: ["Завершена", "Completed"], archived: ["В архиве", "Archived"], cancelled: ["Отменена", "Cancelled"],
  };
  return tr(...labels[status]);
}

const badgeClasses: Record<Conference["status"], string> = {
  draft: styles.badgeUpcoming,
  registration_open: styles.badgeOpen,
  in_progress: styles.badgeOpen,
  registration_closed: styles.badgeUpcoming,
  completed: styles.badgeCompleted,
  archived: styles.badgeCompleted,
  cancelled: styles.badgeCompleted,
};

const dotClasses: Record<Conference["status"], string> = {
  draft: styles.dotUpcoming,
  registration_open: styles.dotOpen,
  in_progress: styles.dotOpen,
  registration_closed: styles.dotUpcoming,
  completed: styles.dotCompleted,
  archived: styles.dotCompleted,
  cancelled: styles.dotCompleted,
};

function formatDateRange(start: string, end: string): string {
  const startDate = new Date(start);
  const endDate = new Date(end);
  const month = endDate.toLocaleString(dateLocale(), { month: "short" });
  const year = endDate.getFullYear();
  return `${startDate.getDate()}–${endDate.getDate()} ${month} ${year}`;
}

function countLabel(count: number, russian: [string, string, string], english: string): string {
  return `${count.toLocaleString(dateLocale())} ${pluralLabel(count, russian, [english, `${english}s`])}`;
}

export function ConferenceCard({ conference }: ConferenceCardProps) {
  const navigate = useNavigate();
  const dates = formatDateRange(conference.start_date, conference.end_date);
  const displayStatus = conference.status === "registration_open" && !conference.registration_open ? "registration_closed" : conference.status;
  const isUpcoming = displayStatus === "registration_closed";
  const coverClass = conference.registration_open
    ? styles.coverOrange
    : conference.format === "hybrid"
      ? styles.coverPurple
      : isUpcoming
        ? styles.coverGray
        : styles.coverDark;

  return (
    <article className={styles.card}>
      <div
        className={`${styles.cover} ${coverClass}`}
        style={conference.cover_image_url ? { backgroundImage: `url(${conference.cover_image_url})` } : undefined}
      >
        <span className={`${styles.badge} ${badgeClasses[displayStatus]}`}>
          <span className={`${styles.badgeDot} ${dotClasses[displayStatus]}`} />
          {statusLabel(displayStatus)}
        </span>
      </div>
      <div className={styles.body}>
        <div className={styles.meta}>
          <img src={calendarIcon} alt={tr("Календарь", "Calendar")} />
          <span className={styles.dates}>{dates}</span>
          <img src={locationIcon} alt={tr("Местоположение", "Location")} />
          <span>{conference.city || (conference.format === "online" ? tr("Онлайн", "Online") : tr("Город уточняется", "City to be confirmed"))}</span>
        </div>
        <h3 className={styles.title}>{localize(conference.title, conference.title_en)}</h3>
        <p className={styles.description}>{localize(conference.description || conference.topics.join(" · "), conference.description_en)}</p>
        <div className={styles.footer}>
          <div className={styles.footerMeta}>
            <span>{conference.participants_count > 0
              ? countLabel(conference.participants_count, ["участник", "участника", "участников"], "participant")
              : tr("Участников пока нет", "No participants yet")}</span>
            <span>{conference.sessions_count > 0
              ? countLabel(conference.sessions_count, ["доклад в программе", "доклада в программе", "докладов в программе"], "scheduled talk")
              : tr("Программа формируется", "Programme coming soon")}</span>
            {isUpcoming && conference.registration_deadline && <span>
              {tr("Дедлайн", "Deadline")} {formatDeadline(conference.registration_deadline)}
            </span>}
          </div>
          <Button
            variant={conference.registration_open ? "primary" : "secondary"}
            onClick={() => navigate(`/conferences/${conference.slug}`)}
            aria-label={`${tr("Открыть конференцию", "Open conference")} “${localize(conference.title, conference.title_en)}”`}
          >
            {isUpcoming ? tr("Подробнее", "Details") : tr("Открыть", "Open")}
          </Button>
        </div>
      </div>
    </article>
  );
}

function formatDeadline(date: string): string {
  return new Date(date).toLocaleDateString(dateLocale(), {
    day: "numeric",
    month: "long",
  });
}
