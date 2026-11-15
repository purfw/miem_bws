import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { apiRequest, readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function VerifyPage() {
  const [params] = useSearchParams();
  const token = params.get("token");
  const { user, loading, refreshSession } = useAuth();
  const verification = useRef<{ token: string; promise: Promise<void> } | null>(null);
  const [message, setMessage] = useState(token ? tr("Подтверждаем адрес…", "Verifying address…") : tr("В ссылке отсутствует токен подтверждения.", "The verification token is missing from the link."));
  const [error, setError] = useState(!token);

  useEffect(() => {
    if (!token || loading) return;
    let active = true;
    if (verification.current?.token !== token) {
      verification.current = { token, promise: apiRequest<void>(`/auth/verify?token=${encodeURIComponent(token)}`) };
    }
    verification.current.promise
      .then(async () => {
        if (!active) return;
        if (user && !user.email_verified) {
          await refreshSession().catch(() => undefined);
        }
        if (active) {
          setError(false);
          setMessage(tr("Email подтверждён. Можно вернуться к мероприятию и зарегистрироваться.", "Email verified. You can return to the event and register."));
        }
      })
      .catch((reason: unknown) => { if (active) { setError(true); setMessage(readableError(reason)); } });
    return () => { active = false; };
  }, [loading, refreshSession, token, user]);

  return <section className={`${styles.page} ${styles.narrow}`}><h1 className={styles.title}>{tr("Подтверждение email", "Email verification")}</h1><div className={styles.panel}><p className={error ? styles.error : styles.success} role={error ? "alert" : "status"}>{message}</p><p><Link className={styles.link} to={user ? "/profile" : "/login"}>{user ? tr("Открыть профиль", "Open profile") : tr("Перейти ко входу", "Go to sign in")}</Link></p></div></section>;
}
