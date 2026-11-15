import { Link, useSearchParams } from "react-router-dom";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function UnsubscribedPage() {
  const [params] = useSearchParams();
  const success = params.get("success") === "true";
  return <section className={`${styles.page} ${styles.narrow}`}>
    <span className={styles.eyebrow}>{tr("Уведомления", "Notifications")}</span>
    <h1 className={styles.title}>{success ? tr("Подписка отключена", "Subscription disabled") : tr("Ссылка недействительна", "Invalid link")}</h1>
    <p className={success ? styles.success : styles.error}>{success ? tr("Письма по этой подписке больше не будут приходить.", "You will no longer receive emails for this subscription.") : tr("Не удалось распознать ссылку отписки. Управлять подписками можно в личном кабинете.", "The unsubscribe link could not be recognized. You can manage subscriptions in your account.")}</p>
    <Link className={styles.link} to="/profile">{tr("Перейти в личный кабинет", "Go to your account")}</Link>
  </section>;
}
