import { useCallback, useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../../auth/useAuth";
import { readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { ButtonLink } from "../../components/ui/Button/ButtonLink";
import { dateLocale, tr } from "../../i18n/runtime";
import { todayLocalISO } from "../../i18n/dates";
import type {
  Application,
  PaginationData,
  Registration,
} from "../../types/conference";
import { useProfileResource } from "./useProfileResource";
import {
  ProfileBadge,
  ProfileEmpty,
  ProfileFeedback,
  ProfileLoadError,
  ProfileSkeleton,
} from "./ProfileUI";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

function applicationStatus(status: Application["status"]) {
  const labels: Record<Application["status"], [string, string]> = {
    submitted: ["Подана", "Submitted"],
    under_review: ["На рассмотрении", "Under review"],
    revision_requested: ["Нужна доработка", "Revision requested"],
    accepted: ["Принята", "Accepted"],
    rejected: ["Отклонена", "Rejected"],
    withdrawn: ["Отозвана", "Withdrawn"],
  };
  return tr(...labels[status]);
}
function registrationStatus(status: Registration["status"]) {
  return status === "confirmed"
    ? tr("Подтверждена", "Confirmed")
    : status === "waitlisted"
      ? tr("Очередь ожидания", "Waitlisted")
      : tr("Отменена", "Cancelled");
}

export function ProfileApplications() {
  const { request } = useAuth();
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [applications, registrations] = await Promise.all([
        request<{ items: Application[] }>("/applications/my", { signal }),
        (async () => {
          const items: Registration[] = [];
          let page = 1;
          let totalPages: number;
          do {
            const result = await request<{
              data: Registration[];
              pagination: PaginationData;
            }>(`/my/registrations?per_page=100&page=${page}`, { signal });
            items.push(...result.data);
            totalPages = result.pagination.total_pages;
            page++;
          } while (page <= totalPages);
          return items;
        })(),
      ]);
      return { applications: applications.items, registrations };
    },
    [request],
  );
  const resource = useProfileResource(load);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  async function cancel(registration: Registration) {
    if (
      !window.confirm(
        tr(
          "Отменить участие в мероприятии?",
          "Cancel participation in this event?",
        ),
      )
    )
      return;
    setBusy(registration.id);
    setError("");
    setMessage("");
    try {
      const result = await request<{ data: Registration }>(
        `/registrations/${registration.id}`,
        { method: "DELETE" },
      );
      resource.setData(
        (data) =>
          data && {
            ...data,
            registrations: data.registrations.map((item) =>
              item.id === registration.id
                ? { ...item, ...result.data, conference: item.conference }
                : item,
            ),
          },
      );
      setMessage(tr("Участие отменено.", "Participation cancelled."));
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function withdraw(application: Application) {
    if (
      !window.confirm(
        tr(
          "Отозвать заявку на доклад?",
          "Withdraw this presentation application?",
        ),
      )
    )
      return;
    setBusy(application.id);
    setError("");
    setMessage("");
    try {
      const result = await request<Application>(
        `/applications/${application.id}/withdraw`,
        { method: "POST" },
      );
      resource.setData(
        (data) =>
          data && {
            ...data,
            applications: data.applications.map((item) =>
              item.id === application.id ? result : item,
            ),
          },
      );
      setMessage(tr("Заявка отозвана.", "Application withdrawn."));
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  if (resource.loading) return <ProfileSkeleton />;
  if (resource.error)
    return <ProfileLoadError error={resource.error} retry={resource.retry} />;
  const data = resource.data;
  if (!data || (!data.applications.length && !data.registrations.length))
    return (
      <ProfileEmpty>
        {tr(
          "Вы ещё не подавали заявки. Найдите мероприятие и зарегистрируйтесь слушателем или подайте доклад.",
          "You have no applications yet. Find an event to attend or submit a presentation.",
        )}
      </ProfileEmpty>
    );
  const entries = [
    ...data.registrations.map((registration) => ({
      kind: "participation" as const,
      date: registration.registered_at,
      item: registration,
    })),
    ...data.applications.map((application) => ({
      kind: "presentation" as const,
      date: application.created_at,
      item: application,
    })),
  ].sort((a, b) => b.date.localeCompare(a.date));
  return (
    <>
      <ProfileFeedback error={error} message={message} />
      <ul className={styles.list}>
        {entries.map((entry) => {
          if (entry.kind === "participation") {
            const item = entry.item;
            const canCancel =
              item.status !== "cancelled" &&
              item.cancellation_count < 3 &&
              Boolean(
                item.conference?.start_date &&
                  item.conference.start_date > todayLocalISO(),
              );
            return (
              <li
                className={`${styles.panel} ${styles.item}`}
                key={`participation:${item.id}`}
              >
                <div className={styles.itemHeader}>
                  <h3>
                    <Link
                      className={page.link}
                      to={`/conferences/${item.conference?.slug ?? item.conference_id}`}
                    >
                      {item.conference?.title ?? tr("Мероприятие", "Event")}
                    </Link>
                  </h3>
                  <ProfileBadge
                    tone={
                      item.status === "confirmed"
                        ? "accepted"
                        : item.status === "waitlisted"
                          ? "active"
                          : "neutral"
                    }
                  >
                    {registrationStatus(item.status)}
                  </ProfileBadge>
                </div>
                <div className={styles.metadata}>
                  <span>{tr("Участие", "Participation")}</span>
                  <time dateTime={entry.date}>
                    {new Date(entry.date).toLocaleDateString(dateLocale())}
                  </time>
                  {item.status === "waitlisted" && (
                    <span>
                      {tr("Позиция в очереди:", "Waitlist position:")}{" "}
                      {item.waitlist_position}
                    </span>
                  )}
                </div>
                <div className={styles.actions}>
                  <ButtonLink
                    to={`/conferences/${item.conference?.slug ?? item.conference_id}`}
                  >
                    {tr("Открыть мероприятие", "Open event")}
                  </ButtonLink>
                  {canCancel && (
                    <Button
                      variant="ghost"
                      disabled={Boolean(busy)}
                      onClick={() => cancel(item)}
                    >
                      {tr("Отменить участие", "Cancel participation")}
                    </Button>
                  )}
                </div>
              </li>
            );
          }
          const item = entry.item;
          return (
            <li
              className={`${styles.panel} ${styles.item}`}
              key={`presentation:${item.id}`}
            >
              <div className={styles.itemHeader}>
                <h3>
                  <Link
                    className={page.link}
                    to={`/conferences/${item.conference_id}`}
                  >
                    {item.conference_title || tr("Мероприятие", "Event")}
                  </Link>
                </h3>
                <ProfileBadge
                  tone={
                    item.status === "accepted"
                      ? "accepted"
                      : ["rejected", "withdrawn"].includes(item.status)
                        ? "neutral"
                        : "active"
                  }
                >
                  {applicationStatus(item.status)}
                </ProfileBadge>
              </div>
              <div className={styles.metadata}>
                <span>
                  {item.form === "listener"
                    ? tr("Участие", "Participation")
                    : item.form === "poster"
                      ? tr("Доклад · постер", "Presentation · poster")
                      : tr("Доклад", "Presentation")}
                </span>
                <time dateTime={entry.date}>
                  {new Date(entry.date).toLocaleDateString(dateLocale())}
                </time>
              </div>
              <p className={styles.wrap}>{item.title}</p>
              <div className={styles.actions}>
                <ButtonLink to={`/applications/${item.id}`}>
                  {tr("Открыть заявку", "Open application")}
                </ButtonLink>
                <ButtonLink
                  variant="ghost"
                  to={`/conferences/${item.conference_id}`}
                >
                  {tr("Мероприятие", "Event")}
                </ButtonLink>
                {!["accepted", "rejected", "withdrawn"].includes(
                  item.status,
                ) && (
                  <Button
                    variant="ghost"
                    disabled={Boolean(busy)}
                    onClick={() => withdraw(item)}
                  >
                    {tr("Отозвать заявку", "Withdraw application")}
                  </Button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
      {data.applications.length >= 100 && (
        <p className={page.notice}>
          {tr(
            "API показывает последние 100 заявок на доклады.",
            "The API returns the latest 100 presentation applications.",
          )}
        </p>
      )}
    </>
  );
}
