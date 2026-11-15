import { useCallback, useState, type FormEvent } from "react";
import { useAuth } from "../../auth/useAuth";
import { readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { ButtonLink } from "../../components/ui/Button/ButtonLink";
import type { OrganizerVerification } from "../../types/conference";
import { dateLocale, tr } from "../../i18n/runtime";
import { useProfileResource } from "./useProfileResource";
import {
  ProfileBadge,
  ProfileFeedback,
  ProfileLoadError,
  ProfileSkeleton,
} from "./ProfileUI";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

export function ProfileOrganizer() {
  const { user, request } = useAuth();
  const load = useCallback(
    async (signal: AbortSignal) =>
      (
        await request<{ data: OrganizerVerification[] }>("/verifications/my", {
          signal,
        })
      ).data,
    [request],
  );
  const resource = useProfileResource(load);
  const [organization, setOrganization] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  async function attach(verification: OrganizerVerification, document: File) {
    const form = new FormData();
    form.append("document", document);
    const result = await request<OrganizerVerification["documents"][number]>(
      `/verifications/${verification.id}/documents`,
      { method: "POST", body: form },
    );
    resource.setData((items) =>
      (items ?? []).map((item) =>
        item.id === verification.id
          ? { ...item, documents: [...item.documents, result] }
          : item,
      ),
    );
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy("submit");
    setError("");
    setMessage("");
    try {
      const result = await request<OrganizerVerification>("/verifications/", {
        method: "POST",
        body: { organization },
      });
      resource.setData((items) => [result, ...(items ?? [])]);
      if (result.status === "pending") {
        if (!file)
          throw new Error(
            tr(
              "Для ручной проверки приложите подтверждающий документ.",
              "Attach a supporting document for manual review.",
            ),
          );
        await attach(result, file);
      }
      setOrganization("");
      setFile(null);
      setMessage(
        result.status === "approved"
          ? tr(
              "Заявка одобрена. Права применяются автоматически; затем обновите сессию.",
              "Request approved. Permissions are being applied automatically; refresh your session afterwards.",
            )
          : tr(
              "Заявка отправлена на модерацию.",
              "The request has been sent for moderation.",
            ),
      );
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function download(id: string) {
    setBusy(id);
    setError("");
    try {
      const result = await request<{ url: string }>(
        `/verification-documents/${id}?redirect=false`,
      );
      window.location.assign(result.url);
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  if (resource.loading) return <ProfileSkeleton />;
  if (resource.error)
    return <ProfileLoadError error={resource.error} retry={resource.retry} />;
  const canManage =
    user &&
    ["verified_organizer", "organizer_member", "admin"].includes(user.role);
  return (
    <>
      <ProfileFeedback error={error} message={message} />
      {canManage && (
        <div>
          <ButtonLink variant="primary" to="/organizer">
            {tr("Кабинет организатора", "Organizer workspace")}
          </ButtonLink>
        </div>
      )}
      {Boolean(resource.data?.length) && (
        <ul className={styles.list}>
          {resource.data?.map((verification) => (
            <li
              className={`${styles.panel} ${styles.item}`}
              key={verification.id}
            >
              <div className={styles.itemHeader}>
                <h3>{verification.organization}</h3>
                <ProfileBadge
                  tone={
                    verification.status === "approved"
                      ? "accepted"
                      : verification.status === "pending"
                        ? "active"
                        : "neutral"
                  }
                >
                  {verification.status === "approved"
                    ? tr("Одобрено", "Approved")
                    : verification.status === "pending"
                      ? tr("На модерации", "Under review")
                      : tr("Отклонено", "Rejected")}
                </ProfileBadge>
              </div>
              <p className={styles.muted}>
                <time dateTime={verification.created_at}>
                  {new Date(verification.created_at).toLocaleDateString(
                    dateLocale(),
                  )}
                </time>{" "}
                · {tr("способ", "method")}: {verification.method}
              </p>
              {verification.rejection_reason && (
                <p>{verification.rejection_reason}</p>
              )}
              {verification.status === "approved" && (
                <p className={styles.muted}>
                  {verification.role_applied
                    ? tr(
                        "Права применены. Войдите заново для обновления роли.",
                        "Permissions applied. Sign in again to refresh your role.",
                      )
                    : tr(
                        "Применяются права организатора.",
                        "Organizer permissions are being applied.",
                      )}
                </p>
              )}
              <div className={styles.actions}>
                {verification.documents.map((document) => (
                  <Button
                    variant="secondary"
                    key={document.id}
                    disabled={Boolean(busy)}
                    onClick={() => download(document.id)}
                    aria-label={`${tr("Скачать", "Download")} ${document.name}`}
                  >
                    {tr("Скачать документ", "Download document")}
                  </Button>
                ))}
              </div>
              {verification.status === "pending" &&
                verification.documents.length === 0 && (
                  <label className={page.field}>
                    {tr("Добавить документ", "Add document")}
                    <input
                      type="file"
                      accept=".pdf,.docx,.png,application/pdf,image/png"
                      disabled={Boolean(busy)}
                      onChange={async (event) => {
                        const document = event.target.files?.[0];
                        if (!document) return;
                        setBusy(verification.id);
                        setError("");
                        try {
                          await attach(verification, document);
                        } catch (reason) {
                          setError(readableError(reason));
                        } finally {
                          setBusy("");
                        }
                      }}
                    />
                  </label>
                )}
            </li>
          ))}
        </ul>
      )}
      {!resource.data?.some(
        (item) => item.status === "pending" || item.status === "approved",
      ) &&
        user?.role !== "verified_organizer" && (
          <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
            <p>
              {tr(
                "Для доверенного домена права выдаются сразу. В остальных случаях приложите приказ, доверенность или другой подтверждающий документ.",
                "Permissions are granted immediately for a trusted domain. Otherwise attach an order, power of attorney or another supporting document.",
              )}
            </p>
            <label className={page.field}>
              {tr("Организация", "Organization")}
              <input
                required
                maxLength={500}
                value={organization}
                onChange={(event) => setOrganization(event.target.value)}
              />
            </label>
            <label className={page.field}>
              {tr(
                "Подтверждающий документ (PDF, DOCX или PNG)",
                "Supporting document (PDF, DOCX or PNG)",
              )}
              <input
                type="file"
                accept=".pdf,.docx,.png,application/pdf,image/png"
                onChange={(event) => setFile(event.target.files?.[0] ?? null)}
              />
            </label>
            <Button
              variant={canManage ? "secondary" : "primary"}
              type="submit"
              disabled={Boolean(busy)}
            >
              {tr("Подать заявку", "Submit request")}
            </Button>
          </form>
        )}
    </>
  );
}
