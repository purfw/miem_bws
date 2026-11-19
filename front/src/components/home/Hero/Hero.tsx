import { useNavigate } from "react-router-dom";
import { Button } from "../../ui/Button/Button";
import { Stats } from "../Stats/Stats";
import styles from "./Hero.module.css";
import type { HomeResponse } from "../../../types/conference";
import { localize, tr } from "../../../i18n/runtime";
import { formatDayRange } from "../../../i18n/dates";

function formatRange(start?: string, end?: string) {
  if (!start || !end) return tr("Даты уточняются", "Dates to be confirmed");
  return formatDayRange(start, end, "long");
}

export function Hero({ data }: { data: HomeResponse | null }) {
  const navigate = useNavigate();
  const featured = data?.heroConference;
  const info = data?.heroInfo;

  return (
    <section className={styles.hero}>
      <button
        type="button"
        className={styles.featuredCard}
        onClick={() => navigate(featured ? `/conferences/${featured.slug}` : "/conferences")}
        aria-label={featured ? `${tr("Открыть конференцию", "Open conference")} “${localize(featured.title, featured.title_en)}”` : tr("Открыть каталог конференций", "Open conference catalogue")}
      >
        <span className={styles.badge}>
          <span className={styles.badgeDot} />
          {featured?.registration_open ? tr("Регистрация открыта", "Registration open") : tr("Каталог конференций", "Conference catalogue")}
        </span>
        <div className={styles.featuredFooter}>
          <span className={styles.featuredMeta}>
            {featured ? `${tr("Ближайшая", "Next")} · ${formatRange(featured.start_date, featured.end_date)} · ${featured.city || tr("онлайн", "online")}` : tr("Найдите подходящее научное событие", "Find the right academic event")}
          </span>
          <h3 className={styles.featuredTitle}>
            {featured ? localize(featured.title, featured.title_en) : tr("Конференции для исследователей и учёных", "Conferences for researchers and academics")}
          </h3>
        </div>
      </button>

      <div className={styles.copy}>
        <span className={styles.eyebrow}>{tr("АКАДЕМИЧЕСКИЙ ПОРТАЛ", "ACADEMIC PORTAL")}</span>
        <h1 className={styles.title}>
          {tr("Конференции для", "Conferences for")}
          <br />
          {tr("исследователей", "researchers")}
          <br />{tr("и учёных", "and academics")}
        </h1>
        <p className={styles.description}>
          {tr("Единое окно для поиска научных событий, подачи заявок и просмотра программ конференций.", "One place to discover academic events, submit applications and explore conference programmes.")}
        </p>
        <div className={styles.actions}>
          <Button variant="primary" onClick={() => navigate("/conferences")}>
            {tr("Найти конференцию", "Find a conference")}
          </Button>
          <Button
            variant="secondary"
            onClick={() => navigate(featured?.registration_open ? `/conferences/${featured.id}/apply` : "/conferences?tab=active")}
          >
            {tr("Подать заявку", "Submit an application")}
          </Button>
        </div>
        <Stats
          participants={info?.stats.participants}
          conferences={info?.stats.conferences}
        />
      </div>
    </section>
  );
}
