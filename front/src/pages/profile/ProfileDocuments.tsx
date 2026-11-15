import { useCallback, useState } from "react";
import { useAuth } from "../../auth/useAuth";
import { readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { ButtonLink } from "../../components/ui/Button/ButtonLink";
import { dateLocale, tr } from "../../i18n/runtime";
import type { ParticipationDocument } from "../../types/conference";
import { useProfileResource } from "./useProfileResource";
import {
  ProfileEmpty,
  ProfileFeedback,
  ProfileLoadError,
  ProfileSkeleton,
} from "./ProfileUI";
import styles from "./Profile.module.css";

export function ProfileDocuments() {
  const { request } = useAuth();
  const load = useCallback(
    async (signal: AbortSignal) =>
      (
        await request<{ data: ParticipationDocument[] }>("/documents/my", {
          signal,
        })
      ).data,
    [request],
  );
  const resource = useProfileResource(load);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  async function download(id: string) {
    setBusy(id);
    setError("");
    try {
      const result = await request<{ url: string }>(
        `/documents/${id}/download`,
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
  return (
    <>
      <ProfileFeedback error={error} />
      {resource.data?.length ? (
        <ul className={styles.list}>
          {resource.data.map((document) => (
            <li className={`${styles.panel} ${styles.item}`} key={document.id}>
              <h3>{document.conference_title}</h3>
              <p className={styles.muted}>
                {tr("Выдан", "Issued")}{" "}
                <time dateTime={document.issued_at}>
                  {new Date(document.issued_at).toLocaleDateString(
                    dateLocale(),
                  )}
                </time>
              </p>
              <div className={styles.actions}>
                <Button
                  variant="secondary"
                  disabled={busy === document.id}
                  onClick={() => download(document.id)}
                >
                  {tr("Скачать PDF", "Download PDF")}
                </Button>
                <ButtonLink
                  variant="ghost"
                  to={`/conferences/${document.conference_id}`}
                >
                  {tr("Мероприятие", "Event")}
                </ButtonLink>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <ProfileEmpty>
          {tr(
            "Документы появятся после завершения мероприятий.",
            "Documents will appear after events are completed.",
          )}
        </ProfileEmpty>
      )}
    </>
  );
}
