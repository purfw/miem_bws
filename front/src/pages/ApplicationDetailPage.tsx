import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useParams } from "react-router-dom";
import { readableError } from "../api/client";
import { safeRedirectUrl } from "../api/safeUrl";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import type { Application, ApplicationFile, ApplicationReview, CoAuthor } from "../types/conference";
import { CoAuthorsEditor } from "../components/application/CoAuthorsEditor";
import { cleanCoAuthors } from "../components/application/coAuthors";
import { tr } from "../i18n/runtime";
import styles from "./Page.module.css";

export function ApplicationDetailPage() {
  const { applicationId = "" } = useParams();
  const { user } = useAuth();
  return <ApplicationDetails key={`${applicationId}:${user?.id}`} id={applicationId} />;
}

function ApplicationDetails({ id }: { id: string }) {
  const { request } = useAuth();
  const [application, setApplication] = useState<Application | null>(null);
  const [reviews, setReviews] = useState<ApplicationReview[]>([]);
  const [reviewsError, setReviewsError] = useState("");
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState({ title: "", abstract: "", keywords: "" });
  const [coAuthors, setCoAuthors] = useState<CoAuthor[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const editable = application?.status === "submitted" || application?.status === "revision_requested";

  useEffect(() => {
    const controller = new AbortController();
    // Reviews are secondary: a failure there must not hide the application itself.
    Promise.allSettled([
      request<Application>(`/applications/${id}`, { signal: controller.signal }),
      request<{ items: ApplicationReview[] }>(`/applications/${id}/reviews`, { signal: controller.signal }),
    ]).then(([item, result]) => {
      if (controller.signal.aborted) return;
      if (item.status === "fulfilled") {
        setApplication(item.value);
        setForm({ title: item.value.title, abstract: item.value.abstract ?? "", keywords: item.value.keywords.join(", ") });
        setCoAuthors(item.value.co_authors ?? []);
      } else {
        setError(readableError(item.reason));
      }
      if (result.status === "fulfilled") setReviews(result.value.items);
      else setReviewsError(readableError(result.reason));
      setLoading(false);
    });
    return () => controller.abort();
  }, [id, request]);

  async function action(operation: () => Promise<void>) {
    if (busy) return;
    setBusy(true); setError(""); setMessage("");
    try { await operation(); } catch (reason) { setError(readableError(reason)); }
    finally { setBusy(false); }
  }

  async function save(resubmit = false) {
    const item = await request<Application>(`/applications/${id}`, { method: "PATCH", body: {
      title: form.title.trim(), abstract: form.abstract.trim(), keywords: form.keywords.split(",").map((word) => word.trim()).filter(Boolean),
      co_authors: cleanCoAuthors(coAuthors),
    } });
    const updated = resubmit ? await request<Application>(`/applications/${id}/resubmit`, { method: "POST" }) : item;
    setApplication((current) => ({ ...updated, files: current?.files ?? [] }));
    setMessage(resubmit ? tr("Заявка повторно отправлена на проверку.", "The application was resubmitted for review.") : tr("Изменения сохранены.", "Changes saved."));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    void action(() => save());
  }

  async function upload(event: FormEvent) {
    event.preventDefault();
    if (!file) return;
    if (file.size > 20 * 1024 * 1024) { setError(tr("Файл должен быть не больше 20 МБ.", "The file must be no larger than 20 MB.")); return; }
    await action(async () => {
      const payload = new FormData(); payload.append("file", file);
      const uploaded = await request<ApplicationFile>(`/applications/${id}/files`, { method: "POST", body: payload });
      setApplication((current) => current && ({ ...current, files: [...(current.files ?? []), uploaded] }));
      setFile(null);
      if (fileInput.current) fileInput.current.value = "";
      setMessage(tr("Файл прикреплён.", "File attached."));
    });
  }

  return <section className={`${styles.page} ${styles.narrow}`}>
    <h1 className={styles.title}>{application?.title ?? tr("Заявка", "Application")}</h1>
    <Link className={styles.link} to="/profile">{tr("Вернуться в кабинет", "Back to account")}</Link>
    {error && <p className={styles.error} role="alert">{error}</p>}
    {message && <p className={styles.success} role="status">{message}</p>}
    {loading && <p className={styles.notice}>{tr("Загружаем заявку…", "Loading application…")}</p>}
    {application && <>
      <p>{application.conference_title}</p>
      <p>{tr("Статус", "Status")}: {({ submitted: tr("Подана", "Submitted"), under_review: tr("На рассмотрении", "Under review"), revision_requested: tr("Нужна доработка", "Revision requested"), accepted: tr("Принята", "Accepted"), rejected: tr("Отклонена", "Rejected"), withdrawn: tr("Отозвана", "Withdrawn") })[application.status]}</p>
      {application.moderator_comment && <p className={styles.notice}>{application.moderator_comment}</p>}
      <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
        <label className={styles.field}>{tr("Название", "Title")}<input required maxLength={500} disabled={!editable || busy} value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} /></label>
        <label className={styles.field}>{tr("Аннотация", "Abstract")}<textarea disabled={!editable || busy} value={form.abstract} onChange={(event) => setForm({ ...form, abstract: event.target.value })} /></label>
        <label className={styles.field}>{tr("Ключевые слова", "Keywords")}<input disabled={!editable || busy} value={form.keywords} onChange={(event) => setForm({ ...form, keywords: event.target.value })} /></label>
        <CoAuthorsEditor value={coAuthors} onChange={setCoAuthors} disabled={!editable || busy} />
        {editable && <Button type="submit" disabled={busy}>{tr("Сохранить", "Save")}</Button>}
        {application.status === "revision_requested" && <Button type="button" disabled={busy || !form.title.trim()} onClick={() => void action(() => save(true))}>{tr("Отправить на проверку повторно", "Resubmit for review")}</Button>}
      </form>
      <h2>{tr("Прикреплённые файлы", "Attached files")}</h2>
      <ul className={styles.list}>{application.files?.map((item) => <li className={styles.listItem} key={item.id}>
        <strong>{item.original_name}</strong><small>{Math.ceil(item.size_bytes / 1024)} KB</small>
        <div className={styles.inlineActions}>
          <Button disabled={busy} variant="secondary" onClick={() => void action(async () => { const result = await request<{ url: string }>(`/files/${item.id}?redirect=false`); window.location.assign(safeRedirectUrl(result.url)); })}>{tr("Скачать", "Download")}</Button>
          {editable && <Button disabled={busy} variant="ghost" onClick={() => { if (window.confirm(tr("Удалить файл?", "Delete this file?"))) void action(async () => { await request<void>(`/files/${item.id}`, { method: "DELETE" }); setApplication((current) => current && ({ ...current, files: current.files?.filter((entry) => entry.id !== item.id) })); }); }}>{tr("Удалить", "Delete")}</Button>}
        </div>
      </li>)}</ul>
      {editable && <form className={`${styles.panel} ${styles.form}`} onSubmit={upload}>
        <label className={styles.field}>{tr("PDF, DOCX или PNG, до 20 МБ", "PDF, DOCX or PNG, up to 20 MB")}<input ref={fileInput} type="file" accept=".pdf,.docx,.png" required disabled={busy} onChange={(event) => setFile(event.target.files?.[0] ?? null)} /></label>
        <Button type="submit" disabled={busy || !file}>{tr("Прикрепить файл", "Attach file")}</Button>
      </form>}
      <h2>{tr("Рецензии", "Reviews")}</h2>
      {reviewsError ? <p className={styles.error} role="alert">{reviewsError}</p> : reviews.length ? <ul className={styles.list}>{reviews.map((review) => <li className={styles.listItem} key={review.id}><strong>{review.reviewer_name} · {review.score}/10</strong><p>{review.summary}</p>{review.strengths && <p>{review.strengths}</p>}{review.weaknesses && <p>{review.weaknesses}</p>}</li>)}</ul> : <p>{tr("Рецензий, доступных автору, пока нет.", "No reviews are available to the author yet.")}</p>}
    </>}
  </section>;
}
