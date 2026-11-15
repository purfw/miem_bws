import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import type { Conference, ScheduleSlot } from "../types/conference";
import styles from "./Page.module.css";
import { dateLocale, localize, tr } from "../i18n/runtime";

const minimumStartDate = "2027-01-01";

function statusLabel(status: Conference["status"]): string {
  const labels: Record<Conference["status"], [string, string]> = {
    draft: ["Черновик", "Draft"], registration_open: ["Опубликовано", "Published"], registration_closed: ["Регистрация закрыта", "Registration closed"],
    in_progress: ["Проводится", "In progress"], completed: ["Завершено", "Completed"], cancelled: ["Отменено", "Cancelled"], archived: ["Архив", "Archived"],
  };
  return tr(...labels[status]);
}

function localDateTime(value: string): string {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

export function OrganizerPage() {
  const { user, request } = useAuth();
  const navigate = useNavigate();
  const [items, setItems] = useState<Conference[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [editingId, setEditingId] = useState("");
  const [form, setForm] = useState({ title: "", title_en: "", slug: "", description: "", description_en: "", format: "offline", start_date: "", end_date: "", registration_deadline: "", max_participants: "", city: "", location: "", topics: "" });
  const [sessions, setSessions] = useState<ScheduleSlot[]>([]);
  const [session, setSession] = useState({ id: "", conference_id: "", title: "", slot_date: "", start_time: "09:00", end_time: "10:00", speaker_name: "", speaker_affiliation: "", room: "", description: "", session_type: "talk", join_url: "", public_at: "" });

  async function load() {
    try {
      const result = await request<{ data: Conference[] }>("/my/conferences");
      setItems(result.data ?? []);
    } catch (reason) {
      setError(readableError(reason));
    }
  }

  async function loadSessions(conferenceId: string) {
    if (!conferenceId) {
      setSessions([]);
      return;
    }
    try {
      const result = await request<{ data: ScheduleSlot[] }>(`/my/conferences/${conferenceId}/sessions`);
      setSessions(result.data ?? []);
    } catch (reason) {
      setError(readableError(reason));
    }
  }

  useEffect(() => {
    request<{ data: Conference[] }>("/my/conferences")
      .then((result) => setItems(result.data ?? []))
      .catch((reason: unknown) => setError(readableError(reason)));
  }, [request]);

  if (!user || !["verified_organizer", "organizer_member", "admin"].includes(user.role)) {
    return <section className={`${styles.page} ${styles.narrow}`}><h1 className={styles.title}>{tr("Кабинет организатора", "Organizer workspace")}</h1><p className={styles.notice}>{tr("Сначала подтвердите полномочия организатора в личном кабинете.", "Verify your organizer permissions in your personal account first.")}</p><Button onClick={() => navigate("/profile")}>{tr("Перейти в профиль", "Go to profile")}</Button></section>;
  }

  function startEdit(conference: Conference) {
    setEditingId(conference.id);
    setForm({
      title: conference.title,
      title_en: conference.title_en ?? "",
      slug: conference.slug,
      description: conference.description ?? "",
      description_en: conference.description_en ?? "",
      format: conference.format,
      start_date: conference.start_date,
      end_date: conference.end_date,
      registration_deadline: conference.registration_deadline ? localDateTime(conference.registration_deadline) : "",
      max_participants: conference.max_participants?.toString() ?? "",
      city: conference.city ?? "",
      location: conference.primary_venue?.address ?? "",
      topics: conference.topics.join(", "),
    });
    window.scrollTo({ top: document.body.scrollHeight / 2, behavior: "smooth" });
  }

  function resetConferenceForm() {
    setEditingId("");
    setForm({ title: "", title_en: "", slug: "", description: "", description_en: "", format: "offline", start_date: "", end_date: "", registration_deadline: "", max_participants: "", city: "", location: "", topics: "" });
  }

  async function saveConference(event: FormEvent) {
    event.preventDefault(); setError(""); setMessage("");
    if (!form.start_date || form.start_date < minimumStartDate) {
      setError(tr("Дата начала мероприятия должна быть не раньше 01.01.2027.", "The event start date must be on or after January 1, 2027."));
      return;
    }
    if (!form.end_date || form.end_date < form.start_date) {
      setError(tr("Дата окончания не может быть раньше даты начала.", "The end date cannot be earlier than the start date."));
      return;
    }
    setBusy(true);
    try {
      await request(editingId ? `/conferences/${editingId}` : "/conferences/", { method: editingId ? "PATCH" : "POST", body: {
        title: form.title, title_en: form.title_en || undefined, slug: form.slug,
        description: form.description || undefined, description_en: form.description_en || undefined, format: form.format,
        start_date: form.start_date, end_date: form.end_date,
        registration_deadline: form.registration_deadline ? new Date(form.registration_deadline).toISOString() : undefined,
        max_participants: form.max_participants ? Number(form.max_participants) : undefined,
        city: form.city || undefined, location: form.location || undefined,
        topics: form.topics.split(",").map((value) => value.trim()).filter(Boolean),
      }});
      setMessage(editingId ? tr("Изменения мероприятия сохранены.", "Event changes have been saved.") : tr("Черновик создан. Проверьте данные и опубликуйте мероприятие.", "Draft created. Review the details and publish the event."));
      resetConferenceForm();
      await load();
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  async function transition(conference: Conference, status: Conference["status"]) {
    if (status === "cancelled" && !window.confirm(`${tr("Отменить", "Cancel")} “${localize(conference.title, conference.title_en)}”? ${tr("Участники получат уведомление.", "Participants will be notified.")}`)) return;
    setBusy(true); setError("");
    try {
      await request(`/conferences/${conference.id}`, { method: "PATCH", body: { status } });
      await load();
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  async function addSession(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(""); setMessage("");
    try {
      const created = await request<{ id: string }>(session.id ? `/sessions/${session.id}` : `/conferences/${session.conference_id}/sessions`, { method: session.id ? "PUT" : "POST", body: {
        title: session.title, slot_date: session.slot_date, start_time: session.start_time, end_time: session.end_time,
        speaker_name: session.speaker_name || undefined, speaker_affiliation: session.speaker_affiliation || undefined,
        room: session.room || undefined, description: session.description || undefined, session_type: session.session_type,
      }});
      if (session.join_url) {
        await request(`/sessions/${session.id || created.id}/link`, { method: "PUT", body: {
          join_url: session.join_url,
          public_at: session.public_at ? new Date(session.public_at).toISOString() : undefined,
        }});
      }
      const conferenceId = session.conference_id;
      setSession({ ...session, id: "", title: "", speaker_name: "", speaker_affiliation: "", room: "", description: "", join_url: "", public_at: "" });
      setMessage(session.id ? tr("Сессия обновлена.", "Session updated.") : tr("Сессия добавлена в программу.", "Session added to the programme."));
      await loadSessions(conferenceId);
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  function editSession(slot: ScheduleSlot) {
    setSession((current) => ({ ...current, id: slot.id, title: slot.title, slot_date: slot.slot_date,
      start_time: slot.start_time.slice(0, 5), end_time: slot.end_time.slice(0, 5), speaker_name: slot.speaker_name ?? "",
      speaker_affiliation: slot.speaker_affiliation ?? "", room: slot.room ?? "", description: slot.description ?? "",
      session_type: slot.session_type ?? "talk", join_url: "", public_at: "" }));
  }

  async function deleteSession(slot: ScheduleSlot) {
    if (!window.confirm(`${tr("Удалить сессию", "Delete session")} “${slot.title}”?`)) return;
    setBusy(true); setError("");
    try {
      await request<void>(`/sessions/${slot.id}`, { method: "DELETE" });
      setMessage(tr("Сессия удалена из программы.", "Session removed from the programme."));
      await loadSessions(session.conference_id);
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  return <section className={styles.page}>
    <span className={styles.eyebrow}>{tr("Управление мероприятиями", "Event management")}</span><h1 className={styles.title}>{tr("Кабинет организатора", "Organizer workspace")}</h1>
    {error && <p className={styles.error} role="alert">{error}</p>}{message && <p className={styles.success}>{message}</p>}
    <div className={styles.content}><h2>{tr("Мои мероприятия", "My events")}</h2></div>
    {items.length ? <ul className={styles.list}>{items.map((conference) => <li className={styles.listItem} key={conference.id}>
      <strong>{localize(conference.title, conference.title_en)}</strong><small>{statusLabel(conference.status)} · {new Date(conference.start_date).toLocaleDateString(dateLocale())}</small>
      <div className={styles.inlineActions}><Button variant="secondary" onClick={() => navigate(`/conferences/${conference.slug}`)}>{tr("Открыть", "Open")}</Button>
        <Button variant="secondary" disabled={busy} onClick={() => startEdit(conference)}>{tr("Редактировать", "Edit")}</Button>
        {conference.status === "draft" && <Button disabled={busy} onClick={() => transition(conference, "registration_open")}>{tr("Опубликовать", "Publish")}</Button>}
        {conference.status === "registration_open" && <Button variant="secondary" disabled={busy} onClick={() => transition(conference, "registration_closed")}>{tr("Закрыть регистрацию", "Close registration")}</Button>}
        {conference.status === "registration_closed" && <Button variant="secondary" disabled={busy} onClick={() => transition(conference, "registration_open")}>{tr("Возобновить регистрацию", "Reopen registration")}</Button>}
        {!["completed", "cancelled", "archived"].includes(conference.status) && <Button variant="ghost" disabled={busy} onClick={() => transition(conference, "cancelled")}>{tr("Отменить", "Cancel")}</Button>}
      </div>
    </li>)}</ul> : <p className={styles.notice}>{tr("Мероприятий пока нет.", "There are no events yet.")}</p>}

    {(user.role !== "organizer_member" || editingId) && <><div className={styles.content}><h2>{editingId ? tr("Редактирование мероприятия", "Edit event") : tr("Новое мероприятие", "New event")}</h2></div>
    <form className={`${styles.panel} ${styles.form}`} onSubmit={saveConference}>
      <label className={styles.field}>{tr("Название", "Title (Russian)")}<input required maxLength={500} value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} /></label>
      <label className={styles.field}>{tr("Название на английском", "Title in English")}<input maxLength={500} value={form.title_en} onChange={(event) => setForm({ ...form, title_en: event.target.value })} /></label>
      <label className={styles.field}>{tr("Короткий адрес (slug)", "Short address (slug)")}<input required pattern="[a-z0-9-]+" value={form.slug} onChange={(event) => setForm({ ...form, slug: event.target.value.toLowerCase() })} /></label>
      <label className={styles.field}>{tr("Описание", "Description (Russian)")}<textarea value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
      <label className={styles.field}>{tr("Описание на английском", "Description in English")}<textarea value={form.description_en} onChange={(event) => setForm({ ...form, description_en: event.target.value })} /></label>
      <div className={styles.row}><label className={styles.field}>{tr("Формат", "Format")}<select value={form.format} onChange={(event) => setForm({ ...form, format: event.target.value })}><option value="offline">{tr("Очно", "In person")}</option><option value="online">{tr("Онлайн", "Online")}</option><option value="hybrid">{tr("Гибрид", "Hybrid")}</option></select></label><label className={styles.field}>{tr("Вместимость", "Capacity")}<input type="number" min="1" value={form.max_participants} onChange={(event) => setForm({ ...form, max_participants: event.target.value })} /></label></div>
      <div className={styles.row}><label className={styles.field}>{tr("Дата начала", "Start date")}<input type="date" required min={minimumStartDate} value={form.start_date} aria-describedby="start-date-policy" onChange={(event) => setForm({ ...form, start_date: event.target.value })} onInvalid={(event) => event.currentTarget.setCustomValidity(tr("Дата начала мероприятия должна быть не раньше 01.01.2027.", "The event start date must be on or after January 1, 2027."))} onInput={(event) => event.currentTarget.setCustomValidity("")} /><small id="start-date-policy">{tr("Не раньше 01.01.2027", "On or after January 1, 2027")}</small></label><label className={styles.field}>{tr("Дата окончания", "End date")}<input type="date" required min={form.start_date && form.start_date >= minimumStartDate ? form.start_date : minimumStartDate} value={form.end_date} onChange={(event) => setForm({ ...form, end_date: event.target.value })} /></label></div>
      <label className={styles.field}>{tr("Регистрация до", "Registration deadline")}<input type="datetime-local" value={form.registration_deadline} onChange={(event) => setForm({ ...form, registration_deadline: event.target.value })} /></label>
      <div className={styles.row}><label className={styles.field}>{tr("Город", "City")}<input value={form.city} onChange={(event) => setForm({ ...form, city: event.target.value })} /></label><label className={styles.field}>{tr("Площадка / адрес", "Venue / address")}<input value={form.location} onChange={(event) => setForm({ ...form, location: event.target.value })} /></label></div>
      <label className={styles.field}>{tr("Тематики через запятую", "Comma-separated topics")}<input value={form.topics} onChange={(event) => setForm({ ...form, topics: event.target.value })} /></label>
      <div className={styles.inlineActions}><Button type="submit" disabled={busy}>{busy ? tr("Сохраняем…", "Saving…") : editingId ? tr("Сохранить изменения", "Save changes") : tr("Создать черновик", "Create draft")}</Button>{editingId && <Button variant="ghost" type="button" disabled={busy} onClick={resetConferenceForm}>{tr("Отменить редактирование", "Cancel editing")}</Button>}</div>
    </form></>}

    {items.length > 0 && <><div className={styles.content}><h2>{session.id ? tr("Редактировать сессию", "Edit session") : tr("Добавить сессию", "Add session")}</h2></div><form className={`${styles.panel} ${styles.form}`} onSubmit={addSession}>
      <label className={styles.field}>{tr("Мероприятие", "Event")}<select required disabled={Boolean(session.id)} value={session.conference_id} onChange={(event) => { const conference_id = event.target.value; setSession({ ...session, conference_id }); void loadSessions(conference_id); }}><option value="">{tr("Выберите", "Select")}</option>{items.filter((item) => item.status !== "cancelled").map((item) => <option key={item.id} value={item.id}>{localize(item.title, item.title_en)}</option>)}</select></label>
      <label className={styles.field}>{tr("Название сессии", "Session title")}<input required value={session.title} onChange={(event) => setSession({ ...session, title: event.target.value })} /></label>
      <div className={styles.row}><label className={styles.field}>{tr("Дата", "Date")}<input type="date" required value={session.slot_date} onChange={(event) => setSession({ ...session, slot_date: event.target.value })} /></label><label className={styles.field}>{tr("Тип", "Type")}<input value={session.session_type} onChange={(event) => setSession({ ...session, session_type: event.target.value })} /></label></div>
      <div className={styles.row}><label className={styles.field}>{tr("Начало", "Start")}<input type="time" required value={session.start_time} onChange={(event) => setSession({ ...session, start_time: event.target.value })} /></label><label className={styles.field}>{tr("Окончание", "End")}<input type="time" required value={session.end_time} onChange={(event) => setSession({ ...session, end_time: event.target.value })} /></label></div>
      <div className={styles.row}><label className={styles.field}>{tr("Докладчик", "Speaker")}<input value={session.speaker_name} onChange={(event) => setSession({ ...session, speaker_name: event.target.value })} /></label><label className={styles.field}>{tr("Организация докладчика", "Speaker affiliation")}<input value={session.speaker_affiliation} onChange={(event) => setSession({ ...session, speaker_affiliation: event.target.value })} /></label></div>
      <label className={styles.field}>{tr("Зал", "Room")}<input value={session.room} onChange={(event) => setSession({ ...session, room: event.target.value })} /></label>
      <label className={styles.field}>{tr("Описание", "Description")}<textarea value={session.description} onChange={(event) => setSession({ ...session, description: event.target.value })} /></label>
      <label className={styles.field}>{session.id ? tr("Новая закрытая ссылка подключения (оставьте пустой, чтобы сохранить текущую)", "New private join link (leave blank to keep the current link)") : tr("Закрытая ссылка подключения (HTTPS)", "Private join link (HTTPS)")}<input type="url" value={session.join_url} onChange={(event) => setSession({ ...session, join_url: event.target.value })} /></label>
      <label className={styles.field}>{tr("Показывать ссылку после", "Show link after")}<input type="datetime-local" value={session.public_at} onChange={(event) => setSession({ ...session, public_at: event.target.value })} /></label>
      <div className={styles.inlineActions}><Button type="submit" disabled={busy}>{busy ? tr("Сохраняем…", "Saving…") : session.id ? tr("Сохранить сессию", "Save session") : tr("Добавить сессию", "Add session")}</Button>{session.id && <Button type="button" variant="ghost" disabled={busy} onClick={() => setSession({ ...session, id: "", title: "", speaker_name: "", speaker_affiliation: "", room: "", description: "", join_url: "", public_at: "" })}>{tr("Отменить редактирование", "Cancel editing")}</Button>}</div>
    </form>
    {session.conference_id && <><div className={styles.content}><h2>{tr("Программа мероприятия", "Event programme")}</h2></div>{sessions.length ? <ul className={styles.list}>{sessions.map((slot) => <li className={styles.listItem} key={slot.id}><strong>{slot.start_time.slice(0, 5)}–{slot.end_time.slice(0, 5)} · {slot.title}</strong><small>{new Date(slot.slot_date).toLocaleDateString(dateLocale())}{slot.speaker_name ? ` · ${slot.speaker_name}` : ""}{slot.room ? ` · ${slot.room}` : ""}</small><div className={styles.inlineActions}><Button type="button" variant="secondary" disabled={busy} onClick={() => editSession(slot)}>{tr("Редактировать", "Edit")}</Button><Button type="button" variant="ghost" disabled={busy} onClick={() => deleteSession(slot)}>{tr("Удалить", "Delete")}</Button></div></li>)}</ul> : <p className={styles.notice}>{tr("В программе пока нет сессий.", "The programme has no sessions yet.")}</p>}</>}
    </>}
  </section>;
}
