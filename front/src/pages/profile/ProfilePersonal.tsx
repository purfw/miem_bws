import { useState, type FormEvent } from "react";
import { useAuth } from "../../auth/useAuth";
import { readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { useLocale } from "../../i18n/useLocale";
import { tr } from "../../i18n/runtime";
import type { User } from "../../types/conference";
import { ProfileFeedback } from "./ProfileUI";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

export function ProfilePersonal() {
  const { user, request, updateUser } = useAuth();
  const { setLocale } = useLocale();
  const [form, setForm] = useState({
    first_name: user?.first_name ?? "",
    last_name: user?.last_name ?? "",
    patronymic: user?.patronymic ?? "",
    affiliation: user?.affiliation ?? "",
    locale: user?.locale ?? "ru",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const updated = await request<User>("/auth/me", {
        method: "PATCH",
        body: form,
      });
      updateUser(updated);
      setMessage(tr("Профиль сохранён.", "Profile saved."));
      setLocale(updated.locale);
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <ProfileFeedback error={error} message={message} />
      <form className={`${styles.panel} ${styles.form}`} onSubmit={save}>
        <span className={styles.avatar} aria-hidden="true">
          {`${user?.first_name[0] ?? ""}${user?.last_name[0] ?? ""}`.toUpperCase()}
        </span>
        <label className={page.field}>
          Email
          <input
            value={user?.email ?? ""}
            type="email"
            readOnly
            autoComplete="email"
          />
        </label>
        <div className={`${page.row} ${styles.formRow}`}>
          <label className={page.field}>
            {tr("Имя", "First name")}
            <input
              required
              autoComplete="given-name"
              maxLength={100}
              value={form.first_name}
              onChange={(event) =>
                setForm({ ...form, first_name: event.target.value })
              }
            />
          </label>
          <label className={page.field}>
            {tr("Фамилия", "Last name")}
            <input
              required
              autoComplete="family-name"
              maxLength={100}
              value={form.last_name}
              onChange={(event) =>
                setForm({ ...form, last_name: event.target.value })
              }
            />
          </label>
        </div>
        <label className={page.field}>
          {tr("Отчество", "Middle name")}
          <input
            autoComplete="additional-name"
            maxLength={100}
            value={form.patronymic}
            onChange={(event) =>
              setForm({ ...form, patronymic: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr("Организация", "Organization")}
          <input
            autoComplete="organization"
            maxLength={255}
            value={form.affiliation}
            onChange={(event) =>
              setForm({ ...form, affiliation: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr("Язык интерфейса", "Interface language")}
          <select
            value={form.locale}
            onChange={(event) =>
              setForm({ ...form, locale: event.target.value as "ru" | "en" })
            }
          >
            <option value="ru">Русский</option>
            <option value="en">English</option>
          </select>
        </label>
        <Button type="submit" disabled={busy}>
          {busy
            ? tr("Сохраняем…", "Saving…")
            : tr("Сохранить профиль", "Save profile")}
        </Button>
      </form>
    </>
  );
}
