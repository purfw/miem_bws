import { useCallback, useState, type FormEvent } from "react";
import { useAuth } from "../../auth/useAuth";
import { readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { ButtonLink } from "../../components/ui/Button/ButtonLink";
import type { Subscription } from "../../types/conference";
import { tr } from "../../i18n/runtime";
import { useProfileResource } from "./useProfileResource";
import {
  ProfileFeedback,
  ProfileLoadError,
  ProfileSkeleton,
} from "./ProfileUI";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

type NotifyOn = Subscription["notify_on"][number];

const notificationTypes: Array<{ value: NotifyOn; label: [string, string] }> = [
  { value: "conference_published", label: ["Новое мероприятие по фильтрам", "New matching event"] },
  { value: "deadline_approaching", label: ["Приближается дедлайн регистрации", "Registration deadline approaching"] },
  { value: "session_updated", label: ["Изменения в программе", "Programme changes"] },
  { value: "speaker_added", label: ["Новые докладчики", "New speakers"] },
  { value: "conference_cancelled", label: ["Отмена мероприятия", "Event cancelled"] },
];

function notificationLabel(value: NotifyOn): string {
  const item = notificationTypes.find((entry) => entry.value === value);
  return item ? tr(...item.label) : value;
}

function formatLabel(value: string): string {
  return value === "offline" ? tr("Очно", "In person") : value === "hybrid" ? tr("Гибрид", "Hybrid") : value === "online" ? tr("Онлайн", "Online") : value;
}

export function ProfileNotifications() {
  const { request } = useAuth();
  const load = useCallback(
    async (signal: AbortSignal) =>
      (await request<{ data: Subscription[] }>("/subscriptions/", { signal }))
        .data,
    [request],
  );
  const resource = useProfileResource(load);
  const [filters, setFilters] = useState({
    topics: "",
    format: "",
    city: "",
    from: "",
    to: "",
  });
  // Filter subscriptions announce new events by default; programme updates
  // for every matching event are opt-in because they can be frequent.
  const [notifyOn, setNotifyOn] = useState<NotifyOn[]>(["conference_published", "deadline_approaching"]);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  async function subscribe(event: FormEvent) {
    event.preventDefault();
    setBusy("filters");
    setError("");
    setMessage("");
    try {
      const criteria = {
        topics: filters.topics
          .split(",")
          .map((item) => item.trim())
          .filter(Boolean),
        formats: filters.format ? [filters.format] : [],
        city: filters.city.trim(),
        from: filters.from,
        to: filters.to,
      };
      const result = await request<{ data: Subscription }>(
        "/subscriptions/criteria",
        {
          method: "POST",
          body: {
            criteria,
            notify_on: notifyOn,
            channels: ["email"],
          },
        },
      );
      resource.setData((items) => [
        result.data,
        ...(items ?? []).filter((item) => item.id !== result.data.id),
      ]);
      setMessage(tr("Подписка сохранена. Письма будут приходить на ваш email.", "Subscription saved. Emails will be sent to your address."));
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function unsubscribe(id: string) {
    setBusy(id);
    setError("");
    setMessage("");
    try {
      await request<void>(`/subscriptions/${id}`, { method: "DELETE" });
      resource.setData((items) =>
        (items ?? []).filter((item) => item.id !== id),
      );
      setMessage(tr("Подписка отключена.", "Subscription disabled."));
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  if (resource.loading) return <ProfileSkeleton />;
  if (resource.error)
    return <ProfileLoadError error={resource.error} retry={resource.retry} />;
  return (
    <>
      <ProfileFeedback error={error} message={message} />
      <form className={`${styles.panel} ${styles.form}`} onSubmit={subscribe}>
        <h3>
          {tr("Подписка на будущие мероприятия", "Subscribe to future events")}
        </h3>
        <p className={styles.muted}>
          {tr(
            "Укажите хотя бы один фильтр: тематику, формат, город или даты.",
            "Specify at least one filter: topic, format, city or dates.",
          )}
        </p>
        <label className={page.field}>
          {tr("Тематики через запятую", "Topics, comma-separated")}
          <input
            maxLength={1000}
            value={filters.topics}
            onChange={(event) =>
              setFilters({ ...filters, topics: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr("Формат", "Format")}
          <select
            value={filters.format}
            onChange={(event) =>
              setFilters({ ...filters, format: event.target.value })
            }
          >
            <option value="">{tr("Любой", "Any")}</option>
            <option value="offline">{tr("Очно", "In person")}</option>
            <option value="hybrid">{tr("Гибрид", "Hybrid")}</option>
            <option value="online">{tr("Онлайн", "Online")}</option>
          </select>
        </label>
        <label className={page.field}>
          {tr("Город", "City")}
          <input
            maxLength={100}
            value={filters.city}
            onChange={(event) =>
              setFilters({ ...filters, city: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr("Дата с", "Date from")}
          <input
            type="date"
            value={filters.from}
            onChange={(event) =>
              setFilters({ ...filters, from: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr("Дата по", "Date to")}
          <input
            type="date"
            min={filters.from || undefined}
            value={filters.to}
            onChange={(event) =>
              setFilters({ ...filters, to: event.target.value })
            }
          />
        </label>
        <fieldset className={styles.form}>
          <legend>{tr("Что присылать", "What to send")}</legend>
          {notificationTypes.map((item) => (
            <label key={item.value}>
              <input
                type="checkbox"
                checked={notifyOn.includes(item.value)}
                onChange={(event) =>
                  setNotifyOn(event.target.checked
                    ? [...notifyOn, item.value]
                    : notifyOn.filter((value) => value !== item.value))
                }
              />{" "}
              {tr(...item.label)}
            </label>
          ))}
        </fieldset>
        <Button type="submit" disabled={Boolean(busy) || notifyOn.length === 0}>
          {tr("Подписаться по фильтрам", "Subscribe to filters")}
        </Button>
      </form>
      {resource.data?.length ? (
        <ul className={styles.list}>
          {resource.data.map((subscription) => (
            <li
              className={`${styles.panel} ${styles.item}`}
              key={subscription.id}
            >
              <h3>
                {subscription.conference_id
                  ? tr("Обновления мероприятия", "Event updates")
                  : tr("Подписка по фильтрам", "Filter subscription")}
              </h3>
              {!subscription.conference_id && (
                <p className={styles.muted}>
                  {[
                    subscription.criteria?.topics?.join(", "),
                    subscription.criteria?.formats?.map(formatLabel).join(", "),
                    subscription.criteria?.city,
                    subscription.criteria?.from,
                    subscription.criteria?.to,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
              )}
              <p className={styles.muted}>
                {subscription.notify_on.map(notificationLabel).join(", ")} · email
              </p>
              <div className={styles.actions}>
                {subscription.conference_id && (
                  <ButtonLink to={`/conferences/${subscription.conference_id}`}>
                    {tr("Открыть мероприятие", "Open event")}
                  </ButtonLink>
                )}
                <Button
                  variant="ghost"
                  disabled={Boolean(busy)}
                  onClick={() => unsubscribe(subscription.id)}
                >
                  {tr("Отписаться", "Unsubscribe")}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className={page.notice}>
          {tr(
            "Активных подписок нет. Используйте фильтры выше или подпишитесь на обновления в карточке мероприятия.",
            "There are no active subscriptions. Use the filters above or subscribe from an event page.",
          )}
        </p>
      )}
    </>
  );
}
