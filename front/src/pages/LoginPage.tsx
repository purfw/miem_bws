import { useEffect, useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { apiRequest, readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function LoginPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const destination = (location.state as { from?: string } | null)?.from || "/profile";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(() => params.get("orcid_error") === "1" ? tr("Не удалось войти через ORCID. Попробуйте ещё раз.", "ORCID sign-in failed. Please try again.") : "");
  const [orcidEnabled, setOrcidEnabled] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    apiRequest<{ orcid: boolean }>("/auth/providers", { signal: controller.signal })
      .then((providers) => { if (!controller.signal.aborted) setOrcidEnabled(providers.orcid); })
      .catch(() => undefined);
    return () => controller.abort();
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(email, password);
      navigate(destination, { replace: true });
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={`${styles.page} ${styles.narrow}`}>
      <span className={styles.eyebrow}>{tr("Личный кабинет", "Personal account")}</span>
      <h1 className={styles.title}>{tr("Вход", "Sign in")}</h1>
      <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
        {error && <p className={styles.error} role="alert">{error}</p>}
        <label className={styles.field}>Email<input type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} /></label>
        <label className={styles.field}>{tr("Пароль", "Password")}<input type="password" autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} /></label>
        <div className={styles.actions}>
          <Button type="submit" disabled={busy}>{busy ? tr("Входим…", "Signing in…") : tr("Войти", "Sign in")}</Button>
          <Link className={styles.link} to="/forgot-password">{tr("Забыли пароль?", "Forgot password?")}</Link>
          {orcidEnabled && <a className={styles.link} href="/api/v1/auth/orcid/start">{tr("Войти через ORCID", "Sign in with ORCID")}</a>}
        </div>
        <p>{tr("Нет аккаунта?", "No account?")} <Link className={styles.link} to="/register" state={{ from: destination }}>{tr("Зарегистрироваться", "Register")}</Link></p>
      </form>
    </section>
  );
}
