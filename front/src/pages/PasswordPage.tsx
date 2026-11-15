import { useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { apiRequest, passwordFieldError, readableError } from "../api/client";
import { Button } from "../components/ui/Button/Button";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";

export function PasswordPage() {
  const [params] = useSearchParams();
  const token = params.get("token");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [done, setDone] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setPasswordError("");
    try {
      if (token) await apiRequest<void>("/auth/reset-password", { method: "POST", body: { token, new_password: password } });
      else await apiRequest<void>("/auth/forgot-password", { method: "POST", body: { email } });
      setDone(true);
    } catch (reason) {
      const fieldError = passwordFieldError(reason);
      if (fieldError) setPasswordError(fieldError);
      else setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={`${styles.page} ${styles.narrow}`}>
      <h1 className={styles.title}>{token ? tr("Новый пароль", "New password") : tr("Восстановление доступа", "Recover access")}</h1>
      <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
        {error && <p className={styles.error}>{error}</p>}
        {done ? <><p className={styles.success}>{token ? tr("Пароль изменён.", "Password changed.") : tr("Если адрес зарегистрирован, письмо поставлено в очередь отправки.", "If the address is registered, the email has been queued.")}</p><Link className={styles.link} to="/login">{tr("Перейти ко входу", "Go to sign in")}</Link></> : <>
          {token ? <label className={styles.field}>{tr("Новый пароль (не менее 12 символов)", "New password (at least 12 characters)")}<input type="password" minLength={12} maxLength={1024} autoComplete="new-password" required aria-invalid={!!passwordError} aria-describedby={passwordError ? "reset-password-error" : undefined} value={password} onChange={(event) => { setPassword(event.target.value); setPasswordError(""); }} /></label> : <label className={styles.field}>Email<input type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} /></label>}
          {passwordError && <p id="reset-password-error" className={styles.error} role="alert">{passwordError}</p>}
          <Button type="submit" disabled={busy}>{busy ? tr("Отправляем…", "Sending…") : token ? tr("Сохранить пароль", "Save password") : tr("Получить ссылку", "Get reset link")}</Button>
        </>}
      </form>
    </section>
  );
}
