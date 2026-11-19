import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { apiRequest, readableError } from "../api/client";
import type { ParticipationDocument } from "../types/conference";
import styles from "./Page.module.css";
import { dateLocale, tr } from "../i18n/runtime";

export function VerifyDocumentPage() {
  const [params] = useSearchParams();
  const code = params.get("code")?.trim() ?? "";
  // Remount per code so a previous result/error is never shown for a new code.
  return <VerifyDocument key={code} code={code} />;
}

function VerifyDocument({ code }: { code: string }) {
  const [document, setDocument] = useState<ParticipationDocument | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!code) return;
    const controller = new AbortController();
    apiRequest<ParticipationDocument>(`/documents/verify/${encodeURIComponent(code)}`, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) setDocument(result); })
      .catch((reason: unknown) => { if (!controller.signal.aborted) setError(readableError(reason)); });
    return () => controller.abort();
  }, [code]);

  return <section className={styles.page}>
    <span className={styles.eyebrow}>{tr("Проверка подлинности", "Authenticity verification")}</span>
    <h1 className={styles.title}>{tr("Документ участника", "Participant document")}</h1>
    {(error || !code) && <p className={styles.error} role="alert">{tr("Документ не подтверждён:", "Document not verified:")} {error || tr("не указан код проверки документа.", "document verification code is missing.")}</p>}
    {!error && code && !document && <p className={styles.notice}>{tr("Проверяем код…", "Verifying code…")}</p>}
    {document && <div className={styles.panel}>
      <p className={styles.success}>{tr("Документ подлинный.", "The document is authentic.")}</p>
      <p><strong>{tr("Участник:", "Participant:")}</strong> {document.participant_name}</p>
      <p><strong>{tr("Мероприятие:", "Event:")}</strong> {document.conference_title}</p>
      <p><strong>{tr("Выдан:", "Issued:")}</strong> {new Date(document.issued_at).toLocaleString(dateLocale())}</p>
    </div>}
  </section>;
}
