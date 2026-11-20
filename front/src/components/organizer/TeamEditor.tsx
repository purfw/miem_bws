import { useEffect, useState, type FormEvent } from "react";
import { ApiError, readableError } from "../../api/client";
import { useAuth } from "../../auth/useAuth";
import { Button } from "../ui/Button/Button";
import { tr } from "../../i18n/runtime";
import page from "../../pages/Page.module.css";

type Permission = "edit" | "schedule" | "participants";

interface Member {
  user_id: string;
  name: string;
  email: string;
  permissions: Permission[];
}

const permissionLabels: Record<Permission, [string, string]> = {
  edit: ["Карточка и материалы", "Event card and content"],
  schedule: ["Программа", "Programme"],
  participants: ["Участники", "Participants"],
};

/** Team of a conference; rendered only for its owner (the API answers 403 otherwise). */
export function TeamEditor({ conferenceId }: { conferenceId: string }) {
  const { request } = useAuth();
  const [members, setMembers] = useState<Member[] | null>(null);
  const [hidden, setHidden] = useState(false);
  const [email, setEmail] = useState("");
  const [permissions, setPermissions] = useState<Permission[]>(["edit", "schedule"]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    request<{ data: Member[] }>(`/conferences/${conferenceId}/members`, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setMembers(result.data); })
      .catch((reason: unknown) => {
        if (controller.signal.aborted) return;
        if (reason instanceof ApiError && reason.status === 403) setHidden(true);
        else setError(readableError(reason));
      });
    return () => controller.abort();
  }, [conferenceId, request]);

  if (hidden) return null;

  async function add(event: FormEvent) {
    event.preventDefault();
    setBusy(true); setError("");
    try {
      const member = await request<Member>(`/conferences/${conferenceId}/members`, { method: "PUT", body: { email: email.trim(), permissions } });
      setMembers((current) => [...(current ?? []).filter((item) => item.user_id !== member.user_id), member]);
      setEmail("");
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  async function remove(member: Member) {
    if (!window.confirm(tr(`Убрать ${member.name || member.email} из команды?`, `Remove ${member.name || member.email} from the team?`))) return;
    setBusy(true); setError("");
    try {
      await request<void>(`/conferences/${conferenceId}/members/${member.user_id}`, { method: "DELETE" });
      setMembers((current) => (current ?? []).filter((item) => item.user_id !== member.user_id));
    } catch (reason) { setError(readableError(reason)); } finally { setBusy(false); }
  }

  return (
    <>
    <div className={page.content}><h2>{tr("Команда мероприятия", "Event team")}</h2></div>
    <div className={page.panel}>
      {error && <p className={page.error} role="alert">{error}</p>}
      {members === null ? <p className={page.notice}>{tr("Загружаем команду…", "Loading team…")}</p> : members.length ? (
        <ul className={page.list}>
          {members.map((member) => (
            <li key={member.user_id} className={page.listItem}>
              <strong>{member.name || member.email || member.user_id}</strong>
              <small>{member.email} · {member.permissions.map((p) => tr(...permissionLabels[p])).join(", ")}</small>
              <div className={page.inlineActions}><Button type="button" variant="ghost" disabled={busy} onClick={() => remove(member)}>{tr("Убрать", "Remove")}</Button></div>
            </li>
          ))}
        </ul>
      ) : <p className={page.notice}>{tr("В команде пока никого нет.", "The team is empty.")}</p>}
      <form className={page.form} onSubmit={add}>
        <p className={page.notice}>{tr("Добавить можно пользователя с ролью «Участник команды организатора» или подтверждённого организатора. Роль выдаёт администратор.", "You can add a user with the organizer team member role or a verified organizer. The role is granted by an administrator.")}</p>
        <label className={page.field}>{tr("Email пользователя", "User email")}<input type="email" required maxLength={255} value={email} onChange={(event) => setEmail(event.target.value)} /></label>
        <fieldset className={page.form}>
          <legend>{tr("Права", "Permissions")}</legend>
          {(Object.keys(permissionLabels) as Permission[]).map((permission) => (
            <label key={permission}>
              <input type="checkbox" checked={permissions.includes(permission)} onChange={(event) => setPermissions(event.target.checked ? [...permissions, permission] : permissions.filter((p) => p !== permission))} />{" "}
              {tr(...permissionLabels[permission])}
            </label>
          ))}
        </fieldset>
        <Button type="submit" disabled={busy || permissions.length === 0}>{tr("Добавить в команду", "Add to team")}</Button>
      </form>
    </div>
    </>
  );
}
