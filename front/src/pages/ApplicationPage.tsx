import { useEffect, useState, type FormEvent } from "react";
import { Link, useParams } from "react-router-dom";
import { readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import type { Application, ConferenceDetail } from "../types/conference";
import styles from "./Page.module.css";
import { localize, tr } from "../i18n/runtime";

export function ApplicationPage() {
  const { conferenceId = "" } = useParams();
  const { request } = useAuth();
  const [conference, setConference] = useState<ConferenceDetail | null>(null);
  const [form, setForm] = useState<"speaker" | "poster">("speaker");
  const [title, setTitle] = useState("");
  const [abstract, setAbstract] = useState("");
  const [sectionId, setSectionId] = useState("");
  const [keywords, setKeywords] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<Application | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    request<ConferenceDetail>(`/conferences/${encodeURIComponent(conferenceId)}`, { signal: controller.signal })
      .then(setConference)
      .catch((reason: unknown) => {
        if (reason instanceof DOMException && reason.name === "AbortError") return;
        setError(readableError(reason));
      });
    return () => controller.abort();
  }, [conferenceId, request]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!conference) return;
    setBusy(true);
    setError("");
    try {
      const application = await request<Application>(`/conferences/${conference.id}/applications`, {
        method: "POST",
        body: {
          form,
          title: title.trim(),
          abstract: abstract.trim() || undefined,
          section_id: sectionId || undefined,
          keywords: keywords.split(",").map((item) => item.trim()).filter(Boolean),
        },
      });
      setCreated(application);
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={`${styles.page} ${styles.narrow}`}>
      <span className={styles.eyebrow}>{tr("Подача доклада", "Presentation submission")}</span>
      <h1 className={styles.title}>{conference ? localize(conference.title, conference.title_en) : tr("Конференция", "Conference")}</h1>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {conference && !conference.registration_open ? (
        <div className={styles.panel}><p className={styles.notice}>{tr("Регистрация на эту конференцию закрыта.", "Registration for this conference is closed.")}</p><Link className={styles.link} to={`/conferences/${conference.slug}`}>{tr("Вернуться к конференции", "Back to conference")}</Link></div>
      ) : created ? (
        <div className={styles.panel}><p className={styles.success}>{tr("Заявка отправлена и получила статус «Подана».", "The application has been submitted.")}</p><div className={styles.actions}><Link className={styles.link} to={`/applications/${created.id}`}>{tr("Открыть заявку и прикрепить файлы", "Open application and attach files")}</Link><Link className={styles.link} to="/profile">{tr("Открыть мои заявки", "View my applications")}</Link><Link className={styles.link} to={`/conferences/${conference?.slug}`}>{tr("Вернуться к конференции", "Back to conference")}</Link></div></div>
      ) : (
        <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
          <p className={styles.notice}>{tr("Для участия без доклада вернитесь к карточке мероприятия и выберите «Зарегистрироваться на мероприятие».", "To attend without a presentation, return to the event page and choose Register for the event.")} <Link className={styles.link} to={`/conferences/${conference?.slug ?? conferenceId}`}>{tr("Открыть мероприятие", "Open event")}</Link></p>
          <label className={styles.field}>{tr("Формат доклада", "Presentation format")}<select value={form} onChange={(event) => setForm(event.target.value as typeof form)}><option value="speaker">{tr("Устный доклад", "Oral presentation")}</option><option value="poster">{tr("Постер", "Poster")}</option></select></label>
          <label className={styles.field}>{tr("Название доклада", "Presentation title")}<input required maxLength={500} value={title} onChange={(event) => setTitle(event.target.value)} /></label>
          {conference?.sections.length ? <label className={styles.field}>{tr("Секция", "Section")}<select value={sectionId} onChange={(event) => setSectionId(event.target.value)}><option value="">{tr("Без секции", "No section")}</option>{conference.sections.map((section) => <option key={section.id} value={section.id}>{localize(section.title, section.title_en)}</option>)}</select></label> : null}
          <label className={styles.field}>{tr("Аннотация", "Abstract")}<textarea minLength={20} value={abstract} onChange={(event) => setAbstract(event.target.value)} /></label>
          <label className={styles.field}>{tr("Ключевые слова через запятую", "Comma-separated keywords")}<input value={keywords} onChange={(event) => setKeywords(event.target.value)} /></label>
          <Button type="submit" disabled={busy || !conference}>{busy ? tr("Отправляем…", "Sending…") : tr("Отправить заявку", "Submit application")}</Button>
        </form>
      )}
    </section>
  );
}
