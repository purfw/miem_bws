import { Link } from "react-router-dom";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function AboutPage() {
  return (
    <section className={styles.page}>
      <span className={styles.eyebrow}>ConferenceHub</span>
      <h1 className={styles.title}>{tr("Научные события в одном месте", "Academic events in one place")}</h1>
      <p className={styles.subtitle}>{tr("Платформа помогает исследователям находить конференции, следить за программой и подавать заявки, а организаторам — собирать материалы участников в едином контуре.", "The platform helps researchers discover conferences, follow programmes and submit applications, while organizers manage participant materials in one system.")}</p>
      <div className={styles.content}>
        <h2>{tr("Что уже доступно", "What is available")}</h2>
        <ul className={styles.list}>
          <li className={styles.listItem}><strong>{tr("Каталог и поиск", "Catalogue and search")}</strong><small>{tr("Фильтры по тематике, формату, городу и датам.", "Filter by topic, format, city and date.")}</small></li>
          <li className={styles.listItem}><strong>{tr("Заявки участников", "Participant applications")}</strong><small>{tr("Устный доклад, постер или участие слушателем.", "Oral presentation, poster or attendee participation.")}</small></li>
          <li className={styles.listItem}><strong>{tr("Личный кабинет", "Personal account")}</strong><small>{tr("Статусы заявок и избранные конференции.", "Application statuses and favorite conferences.")}</small></li>
          <li className={styles.listItem}><strong>{tr("Публичная программа", "Public programme")}</strong><small>{tr("Секции, расписание, комитет и партнёры конференции.", "Sections, schedule, committee and conference partners.")}</small></li>
        </ul>
        <h2>{tr("Нужна помощь?", "Need help?")}</h2>
        <p>{tr("Напишите через", "Use the")} <Link className={styles.link} to="/support">{tr("форму поддержки", "support form")}</Link> — {tr("обращение будет зарегистрировано в системе.", "your request will be recorded in the system.")}</p>
      </div>
    </section>
  );
}
