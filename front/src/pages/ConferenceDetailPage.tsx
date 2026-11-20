import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { ApiError, readableError } from "../api/client";
import { safeRedirectUrl, safeUrl } from "../api/safeUrl";
import { ProgramView } from "../components/conference/ProgramView";
import { Recommendations } from "../components/recommendations/Recommendations";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import { ButtonLink } from "../components/ui/Button/ButtonLink";
import buttonStyles from "../components/ui/Button/Button.module.css";
import calendarIcon from "../assets/icons/calendar.svg";
import layout from "./ConferenceDetailPage.module.css";
import type { ConferenceDetail, Registration, Subscription } from "../types/conference";
import styles from "./Page.module.css";
import { dateLocale, localize, tr } from "../i18n/runtime";
import { parseDate, todayLocalISO } from "../i18n/dates";

type DetailTab = "about" | "program" | "organizers" | "partners";

function dateRange(start: string, end: string) {
  const from = parseDate(start);
  const to = parseDate(end);
  return `${from.toLocaleDateString(dateLocale())} — ${to.toLocaleDateString(dateLocale())}`;
}

export function ConferenceDetailPage() {
  const { idOrSlug = "" } = useParams();
  const { user } = useAuth();
  return <ConferenceDetails key={`${idOrSlug}:${user?.id ?? "anonymous"}`} />;
}

function ConferenceDetails() {
  const { idOrSlug = "" } = useParams();
  const { user, request, loading: authLoading } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [conference, setConference] = useState<ConferenceDetail | null>(null);
  const [tab, setTab] = useState<DetailTab>("about");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [favoriteBusy, setFavoriteBusy] = useState(false);
  const [registration, setRegistration] = useState<Registration | null>(null);
  const [registrationBusy, setRegistrationBusy] = useState(false);
  const [registrationLoading, setRegistrationLoading] = useState(Boolean(user));
  const [registrationError, setRegistrationError] = useState("");
  const [registrationAttempt, setRegistrationAttempt] = useState(0);
  const [subscription, setSubscription] = useState<Subscription | null>(null);
  const [subscriptionBusy, setSubscriptionBusy] = useState(false);
  const [subscriptionLoading, setSubscriptionLoading] = useState(Boolean(user));
  const [joinBusy, setJoinBusy] = useState("");
  const conferenceId = conference?.id;
  const userId = user?.id;

  useEffect(() => {
    const controller = new AbortController();
    request<ConferenceDetail>(`/conferences/${encodeURIComponent(idOrSlug)}`, { signal: controller.signal })
      .then(setConference)
      .catch((reason: unknown) => {
        if (reason instanceof DOMException && reason.name === "AbortError") return;
        setError(readableError(reason));
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [idOrSlug, request, userId]);

  useEffect(() => {
    if (!userId || !conferenceId) return;
    const controller = new AbortController();
    request<{ data: Registration | null }>(`/conferences/${conferenceId}/registration`, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) { setRegistration(result.data); setRegistrationError(""); } })
      .catch((reason: unknown) => { if (!controller.signal.aborted) setRegistrationError(readableError(reason)); })
      .finally(() => { if (!controller.signal.aborted) setRegistrationLoading(false); });
    return () => controller.abort();
  }, [conferenceId, registrationAttempt, request, userId]);

  useEffect(() => {
    if (!userId || !conferenceId) return;
    const controller = new AbortController();
    request<{ data: Subscription[] }>("/subscriptions/", { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setSubscription(result.data.find((item) => item.conference_id === conferenceId) ?? null); })
      .catch(() => { if (!controller.signal.aborted) setSubscription(null); })
      .finally(() => { if (!controller.signal.aborted) setSubscriptionLoading(false); });
    return () => controller.abort();
  }, [conferenceId, request, userId]);

  async function toggleFavorite() {
    if (!conference) return;
    if (!user) {
      navigate("/login", { state: { from: location.pathname } });
      return;
    }
    setFavoriteBusy(true);
    setError("");
    try {
      await request<void>(`/conferences/${conference.slug}/favorite`, { method: conference.is_favorite ? "DELETE" : "PUT" });
      setConference({
        ...conference,
        is_favorite: !conference.is_favorite,
        favorites_count: Math.max(0, conference.favorites_count + (conference.is_favorite ? -1 : 1)),
      });
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setFavoriteBusy(false);
    }
  }

  async function registerForConference() {
    if (!conference || registrationBusy || registrationLoading || registrationError) return;
    if (!user) {
      navigate("/login", { state: { from: location.pathname } });
      return;
    }
    if (!user.email_verified) {
      navigate("/profile/security", { state: { from: location.pathname, verifyEmail: true } });
      return;
    }
    setRegistrationBusy(true);
    setError("");
    try {
      const result = await request<{ data: Registration }>(`/conferences/${conference.id}/register`, {
        method: "POST",
        body: { role_at_conf: "attendee" },
      });
      setRegistration(result.data);
    } catch (reason) {
      setError(readableError(reason));
      if (reason instanceof ApiError && reason.problem?.code === "ALREADY_REGISTERED") {
        try {
          const current = await request<{ data: Registration | null }>(`/conferences/${conference.id}/registration`);
          setRegistration(current.data);
        } catch { setRegistrationError(tr("Не удалось обновить статус участия. Обновите страницу.", "Could not refresh your participation status. Reload the page.")); }
      }
    } finally {
      setRegistrationBusy(false);
    }
  }

  async function cancelParticipation() {
    if (!registration || registration.status === "cancelled" || registrationBusy) return;
    if (!window.confirm(tr("Отменить участие в мероприятии?", "Cancel your participation in this event?"))) return;
    setRegistrationBusy(true);
    setError("");
    try {
      const result = await request<{ data: Registration }>(`/registrations/${registration.id}`, { method: "DELETE" });
      setRegistration(result.data);
    } catch (reason) { setError(readableError(reason)); }
    finally { setRegistrationBusy(false); }
  }

  async function toggleSubscription() {
    if (!conference) return;
    if (!user) {
      navigate("/login", { state: { from: location.pathname } });
      return;
    }
    setSubscriptionBusy(true);
    setError("");
    try {
      if (subscription) {
        await request<void>(`/subscriptions/${subscription.id}`, { method: "DELETE" });
        setSubscription(null);
      } else {
        const result = await request<{ data: Subscription }>("/subscriptions/", {
          method: "POST",
          body: { conference_id: conference.id, notify_on: ["deadline_approaching", "session_updated", "speaker_added", "conference_cancelled"], channels: ["email"] },
        });
        setSubscription(result.data);
      }
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setSubscriptionBusy(false);
    }
  }

  async function joinSession(sessionId: string) {
    setJoinBusy(sessionId);
    setError("");
    try {
      const result = await request<{ url: string }>(`/sessions/${sessionId}/join`);
      window.location.assign(safeRedirectUrl(result.url));
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setJoinBusy("");
    }
  }

  if (loading) return <section className={`${styles.page} ${styles.narrow}`}><p className={styles.notice}>{tr("Загружаем конференцию…", "Loading conference…")}</p></section>;
  if (!conference) return <section className={`${styles.page} ${styles.narrow}`}><h1 className={styles.title}>{tr("Конференция не найдена", "Conference not found")}</h1><p className={styles.error}>{error}</p><ButtonLink to="/conferences">{tr("Вернуться в каталог", "Back to catalogue")}</ButtonLink></section>;

  const currentRegistration = user && registration?.conference_id === conference.id ? registration : null;
  const activeRegistration = currentRegistration?.status !== "cancelled" ? currentRegistration : null;
  const cancellationLimitReached = (currentRegistration?.cancellation_count ?? 0) >= 3;
  const canCancel = Boolean(activeRegistration) && !cancellationLimitReached && conference.start_date > todayLocalISO();

  const tabs: Array<{ id: DetailTab; label: string }> = [
    { id: "about", label: tr("О конференции", "About") },
    { id: "program", label: `${tr("Программа", "Programme")} (${conference.schedule.length})` },
    { id: "organizers", label: tr("Организаторы", "Organizers") },
    { id: "partners", label: tr("Партнёры", "Partners") },
  ];

  return (
    <section className={`${styles.page} ${layout.page}`}>
      <div className={layout.layout}>
      <header className={`${styles.detailHero} ${layout.hero}`}>
        <div>
          <div className={styles.detailMeta}>
            <span>{dateRange(conference.start_date, conference.end_date)}</span>
            <span>{conference.city || (conference.format === "online" ? tr("Онлайн", "Online") : tr("Место уточняется", "Location to be confirmed"))}</span>
            <span>{conference.topics.join(" · ")}</span>
          </div>
          <h1 className={styles.detailTitle}>{localize(conference.title, conference.title_en)}</h1>
        </div>
      </header>
      <aside className={layout.actionCard} aria-label={tr("Участие в мероприятии", "Event participation")}>
        <dl className={layout.facts}>
          <div><dt>{tr("Дата", "Date")}</dt><dd>{dateRange(conference.start_date, conference.end_date)}</dd></div>
          <div><dt>{tr("Место и формат", "Location and format")}</dt><dd>{conference.city || tr("Место уточняется", "Location to be confirmed")} · {conference.format === "online" ? tr("Онлайн", "Online") : conference.format === "hybrid" ? tr("Гибрид", "Hybrid") : tr("Очно", "In person")}</dd></div>
          {conference.primary_venue?.address && <div><dt>{tr("Площадка", "Venue")}</dt><dd>{conference.primary_venue.address}</dd></div>}
        </dl>
        <div className={layout.mainActions}>
          {authLoading && conference.registration_open && !activeRegistration && <Button disabled>{tr("Проверяем сессию…", "Checking your session…")}</Button>}
          {!conference.registration_open && !activeRegistration && <Button disabled>{tr("Регистрация закрыта", "Registration closed")}</Button>}
          {!user && !authLoading && conference.registration_open && <ButtonLink variant="primary" to="/register" state={{ from: location.pathname }}>{tr("Зарегистрироваться", "Register")}</ButtonLink>}
          {user && conference.registration_open && !activeRegistration && <Button aria-label={tr("Зарегистрироваться на мероприятие", "Register for the event")} onClick={registerForConference} disabled={registrationBusy || registrationLoading || Boolean(registrationError) || cancellationLimitReached}>{registrationLoading ? tr("Проверяем участие…", "Checking participation…") : registrationBusy ? tr("Регистрируем…", "Registering…") : <><span className={layout.fullLabel}>{tr("Зарегистрироваться на мероприятие", "Register for the event")}</span><span className={layout.shortLabel}>{tr("Зарегистрироваться", "Register")}</span></>}</Button>}
          {activeRegistration?.status === "confirmed" && <Button disabled>{tr("✓ Вы зарегистрированы", "✓ Registered")}</Button>}
          {activeRegistration?.status === "waitlisted" && <Button disabled>{tr("Очередь: место", "Waitlist position:")} {activeRegistration.waitlist_position}</Button>}
          {conference.registration_open && <ButtonLink to={`/conferences/${conference.id}/apply`}>{tr("Подать доклад", "Submit a presentation")}</ButtonLink>}
          {activeRegistration && <Button variant="ghost" onClick={cancelParticipation} disabled={registrationBusy || !canCancel}>{registrationBusy ? tr("Сохраняем…", "Saving…") : tr("Отменить участие", "Cancel participation")}</Button>}
        </div>
        {activeRegistration && !canCancel && <p className={layout.notice}>{cancellationLimitReached ? tr("Достигнут лимит отмен участия.", "The cancellation limit has been reached.") : tr("Мероприятие уже началось; отмена участия недоступна.", "The event has started; participation cannot be cancelled.")}</p>}
        {!conference.registration_open && <p className={layout.notice} role="status">{tr("Регистрация на мероприятие и приём докладов закрыты.", "Event registration and presentation submissions are closed.")}</p>}
        <div className={layout.tertiaryActions}>
          <Button className={conference.is_favorite ? layout.activeAction : ""} variant="ghost" onClick={toggleFavorite} disabled={favoriteBusy} aria-pressed={Boolean(conference.is_favorite)} aria-label={conference.is_favorite ? tr("Удалить из избранного", "Remove from favorites") : tr("В избранное", "Add to favorites")} title={conference.is_favorite ? tr("Удалить из избранного", "Remove from favorites") : tr("В избранное", "Add to favorites")}><span aria-hidden="true">{conference.is_favorite ? "★" : "☆"}</span><span className={layout.auxiliaryLabel}>{tr("Избранное", "Favorite")}</span></Button>
          <Button className={subscription ? layout.activeAction : ""} variant="ghost" onClick={toggleSubscription} disabled={subscriptionBusy || subscriptionLoading} aria-pressed={Boolean(subscription)} aria-label={subscription ? tr("Отключить обновления", "Disable updates") : tr("Получать обновления", "Get updates")} title={subscription ? tr("Отключить обновления", "Disable updates") : tr("Получать обновления", "Get updates")}><span aria-hidden="true">{subscription ? "✓" : "+"}</span><span className={layout.auxiliaryLabel}>{tr("Обновления", "Updates")}</span></Button>
          <a className={`${buttonStyles.button} ${buttonStyles.ghost}`} href={`/api/v1/conferences/${conference.slug}/calendar.ics`} aria-label={tr("Добавить в календарь", "Add to calendar")} title={tr("Добавить в календарь", "Add to calendar")}><img className={layout.icon} src={calendarIcon} alt="" /><span className={layout.auxiliaryLabel}>{tr("Календарь", "Calendar")}</span></a>
        </div>
      </aside>
      <div className={layout.body}>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {registrationError && <div className={styles.error} role="alert">{registrationError} <button className={styles.textButton} type="button" onClick={() => { setRegistrationLoading(true); setRegistrationError(""); setRegistrationAttempt((attempt) => attempt + 1); }}>{tr("Повторить проверку участия", "Retry participation check")}</button></div>}
      {user && !user.email_verified && <p className={styles.notice}>{tr("Для регистрации слушателем нужно подтвердить email. Подача доклада — отдельное действие.", "Please verify your email to register as an attendee. Presentation submission is a separate action.")} <Link className={styles.link} to="/profile/security" state={{ from: location.pathname, verifyEmail: true }}>{tr("Подтвердить email в профиле", "Verify email in your profile")}</Link></p>}

      <div className={`${styles.tabs} ${layout.tabs}`} role="tablist" aria-label={tr("Информация о мероприятии", "Event information")}>
        {tabs.map((item) => (
          <button key={item.id} type="button" role="tab" id={`event-tab-${item.id}`} aria-controls="event-tab-panel" tabIndex={tab === item.id ? 0 : -1} aria-selected={tab === item.id} className={`${styles.tab} ${tab === item.id ? styles.tabActive : ""}`} onClick={() => setTab(item.id)} onKeyDown={(event) => {
            const index = tabs.findIndex((entry) => entry.id === tab);
            const next = event.key === "ArrowRight" ? (index + 1) % tabs.length : event.key === "ArrowLeft" ? (index + tabs.length - 1) % tabs.length : event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : -1;
            if (next >= 0) { event.preventDefault(); setTab(tabs[next].id); document.getElementById(`event-tab-${tabs[next].id}`)?.focus(); }
          }}>{item.label}</button>
        ))}
      </div>

      <div className={styles.content} role="tabpanel" id="event-tab-panel" aria-labelledby={`event-tab-${tab}`}>
        {tab === "about" && (
          <>
            <h2>{tr("О конференции", "About the conference")}</h2>
            <p>{conference.description ? localize(conference.description, conference.description_en) : tr("Организаторы пока не добавили описание.", "The organizers have not added a description yet.")}</p>
            <h2>{tr("Участие", "Participation")}</h2>
            <p>{conference.participants_count} {tr("участников", "participants")} · {conference.sessions_count} {tr("событий в программе", "programme events")} · {conference.favorites_count} {tr("добавили в избранное", "favorites")}.</p>
            {activeRegistration?.status === "confirmed" && <p className={styles.success}>{tr("Ваша регистрация подтверждена. Письмо с файлом календаря поставлено в очередь отправки.", "Your registration is confirmed. An email with a calendar file has been queued.")}</p>}
            {activeRegistration?.status === "waitlisted" && <p className={styles.notice}>{tr("Свободных мест пока нет. Ваша позиция в очереди:", "There are no places available yet. Your waitlist position is:")} {activeRegistration.waitlist_position}.</p>}
            {currentRegistration?.status === "cancelled" && <p className={styles.notice}>{tr("Ваше участие отменено.", "Your participation is cancelled.")}</p>}
            {cancellationLimitReached && <p className={styles.notice}>{tr("Достигнут лимит отмен участия в этом мероприятии.", "The cancellation limit for this event has been reached.")}</p>}
            {activeRegistration && !canCancel && !cancellationLimitReached && <p className={styles.notice}>{tr("Мероприятие уже началось; отмена участия недоступна.", "The event has already started; participation cannot be cancelled.")}</p>}
            {conference.registration_deadline && <p>{tr("Приём заявок до", "Applications accepted until")} {new Date(conference.registration_deadline).toLocaleString(dateLocale())}.</p>}
          </>
        )}
        {tab === "program" && (
          <>
            <h2>{tr("Программа", "Programme")}</h2>
            <ProgramView
              schedule={conference.schedule}
              sections={conference.sections}
              onJoin={activeRegistration?.status === "confirmed" ? joinSession : undefined}
              joinBusy={joinBusy}
            />
          </>
        )}
        {tab === "organizers" && (
          <><h2>{tr("Организационный комитет", "Organizing committee")}</h2>{conference.organizers.length ? <ul className={styles.list}>{conference.organizers.map((person) => <li className={styles.listItem} key={person.id}><strong>{person.name}</strong><small>{[person.job_title, person.affiliation].filter(Boolean).join(" · ")}</small></li>)}</ul> : <p>{tr("Состав оргкомитета ещё не опубликован.", "The organizing committee has not been published yet.")}</p>}<h2>{tr("Программный комитет", "Programme committee")}</h2>{conference.program_committee.length ? <ul className={styles.list}>{conference.program_committee.map((person) => <li className={styles.listItem} key={person.id}><strong>{person.name}</strong><small>{person.affiliation}</small></li>)}</ul> : <p>{tr("Состав программного комитета ещё не опубликован.", "The programme committee has not been published yet.")}</p>}</>
        )}
        {tab === "partners" && (
          <><h2>{tr("Партнёры", "Partners")}</h2>{conference.partners.length ? <ul className={styles.list}>{conference.partners.map((partner) => {
            const website = safeUrl(partner.website_url);
            const logo = safeUrl(partner.logo_url);
            return <li className={`${styles.listItem} ${layout.partner}`} key={partner.id}>{logo && <img className={layout.partnerLogo} src={logo} alt="" loading="lazy" />}<strong>{partner.name}</strong>{partner.tier && <small>{partner.tier}</small>}{website && <a className={styles.link} href={website} target="_blank" rel="noreferrer noopener">{tr("Открыть сайт", "Open website")}</a>}</li>;
          })}</ul> : <p>{tr("Партнёры пока не указаны.", "No partners have been listed yet.")}</p>}</>
        )}
      </div>
      <Recommendations
        source={`/conferences/${conference.id}/similar`}
        eyebrow={tr("Ещё по теме", "More like this")}
        title={tr("Похожие конференции", "Similar conferences")}
      />
      </div>
      </div>
    </section>
  );
}
