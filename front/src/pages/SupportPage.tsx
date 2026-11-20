import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { apiRequest, readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function SupportPage() {
  const { user } = useAuth();
  const [form, setForm] = useState({
    name: user ? `${user.first_name} ${user.last_name}` : "",
    email: user?.email ?? "",
    topic: "other",
    subject: "",
    message: "",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  function update(field: keyof typeof form, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await apiRequest<{ id: string; status: string }>("/support/messages", { method: "POST", body: form });
      setDone(true);
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={`${styles.page} ${styles.narrow}`}>
      <span className={styles.eyebrow}>{tr("Помощь", "Help")}</span>
      <h1 className={styles.title}>{tr("Поддержка", "Support")}</h1>
      <p className={styles.subtitle}>{tr("Опишите вопрос — обращение сохранится в системе, даже если почтовая доставка временно недоступна.", "Describe your question. The request will remain in the system even if email delivery is temporarily unavailable.")}</p>
      {done ? <div className={styles.panel}><p className={styles.success}>{tr("Обращение принято.", "Request received.")}</p><Button variant="secondary" onClick={() => { setDone(false); setForm((current) => ({ ...current, subject: "", message: "" })); }}>{tr("Отправить ещё одно", "Send another")}</Button></div> : <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
        {error && <p className={styles.error} role="alert">{error}</p>}
        <div className={styles.row}>
          <label className={styles.field}>{tr("Имя", "Name")}<input required minLength={2} maxLength={255} value={form.name} onChange={(event) => update("name", event.target.value)} /></label>
          <label className={styles.field}>Email<input type="email" required maxLength={255} value={form.email} onChange={(event) => update("email", event.target.value)} /></label>
        </div>
        <label className={styles.field}>{tr("Тема", "Topic")}<select value={form.topic} onChange={(event) => update("topic", event.target.value)}><option value="account">{tr("Аккаунт", "Account")}</option><option value="application">{tr("Заявка", "Application")}</option><option value="conference">{tr("Конференция", "Conference")}</option><option value="bug">{tr("Ошибка на сайте", "Website issue")}</option><option value="other">{tr("Другое", "Other")}</option></select></label>
        <label className={styles.field}>{tr("Заголовок", "Subject")}<input maxLength={500} value={form.subject} onChange={(event) => update("subject", event.target.value)} /></label>
        <label className={styles.field}>{tr("Сообщение", "Message")}<textarea required minLength={10} maxLength={5000} value={form.message} onChange={(event) => update("message", event.target.value)} /></label>
        <Button type="submit" disabled={busy}>{busy ? tr("Отправляем…", "Sending…") : tr("Отправить обращение", "Send request")}</Button>
      </form>}

      <div className={styles.content}><h2>{tr("Частые вопросы", "Frequently asked questions")}</h2></div>
      <div className={styles.faq}>
        <details><summary>{tr("Где посмотреть статус заявки?", "Where can I see my application status?")}</summary><p>{tr("Статусы участия и докладов доступны в профиле, в разделе", "Participation and presentation statuses are available in your profile under")} <Link className={styles.link} to="/profile/applications">{tr("Мои заявки", "My applications")}</Link>.</p></details>
        <details><summary>{tr("Как изменить пароль?", "How do I change my password?")}</summary><p>{tr("На странице входа нажмите «Забыли пароль?» и укажите email аккаунта.", "Select Forgot password on the sign-in page and enter your account email.")}</p></details>
        <details><summary>{tr("Почему кнопка подачи заявки недоступна?", "Why is the application button unavailable?")}</summary><p>{tr("Заявки принимаются только пока организатор держит регистрацию открытой и не истёк дедлайн.", "Applications are accepted only while registration is open and the deadline has not passed.")}</p></details>
      </div>
    </section>
  );
}
