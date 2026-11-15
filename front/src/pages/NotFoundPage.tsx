import { Link } from "react-router-dom";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function NotFoundPage() {
  return <section className={`${styles.page} ${styles.narrow}`}><span className={styles.eyebrow}>{tr("Ошибка 404", "Error 404")}</span><h1 className={styles.title}>{tr("Страница не найдена", "Page not found")}</h1><p className={styles.subtitle}>{tr("Адрес мог измениться или в ссылке есть опечатка.", "The address may have changed or the link may contain a typo.")}</p><p className={styles.panel}><Link className={styles.link} to="/">{tr("Вернуться на главную", "Back to home")}</Link></p></section>;
}
