import { useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { passwordFieldError, readableError } from "../api/client";
import { useAuth } from "../auth/useAuth";
import { Button } from "../components/ui/Button/Button";
import styles from "./Page.module.css";
import { tr } from "../i18n/runtime";
import { useLocale } from "../i18n/useLocale";

export function RegisterPage() {
  const { register, login } = useAuth();
	const { locale } = useLocale();
  const navigate = useNavigate();
  const location = useLocation();
  const destination = (location.state as { from?: string } | null)?.from || "/profile";
  const [form, setForm] = useState({ first_name: "", last_name: "", email: "", affiliation: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [passwordError, setPasswordError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setPasswordError("");
    try {
      await register({ ...form, locale });
      await login(form.email, form.password);
      navigate(destination, { replace: true });
    } catch (reason) {
      const fieldError = passwordFieldError(reason);
      if (fieldError) setPasswordError(fieldError);
      else setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }

  function update(field: keyof typeof form, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  return (
    <section className={`${styles.page} ${styles.narrow}`}>
      <span className={styles.eyebrow}>{tr("Личный кабинет", "Personal account")}</span>
      <h1 className={styles.title}>{tr("Регистрация", "Registration")}</h1>
      <form className={`${styles.panel} ${styles.form}`} onSubmit={submit}>
        {error && <p className={styles.error} role="alert">{error}</p>}
        <div className={styles.row}>
          <label className={styles.field}>{tr("Имя", "First name")}<input autoComplete="given-name" required value={form.first_name} onChange={(event) => update("first_name", event.target.value)} /></label>
          <label className={styles.field}>{tr("Фамилия", "Last name")}<input autoComplete="family-name" required value={form.last_name} onChange={(event) => update("last_name", event.target.value)} /></label>
        </div>
        <label className={styles.field}>{tr("Организация", "Organization")}<input autoComplete="organization" value={form.affiliation} onChange={(event) => update("affiliation", event.target.value)} /></label>
        <label className={styles.field}>Email<input type="email" autoComplete="email" required value={form.email} onChange={(event) => update("email", event.target.value)} /></label>
        <label className={styles.field}>{tr("Пароль (не менее 12 символов)", "Password (at least 12 characters)")}<input type="password" autoComplete="new-password" minLength={12} maxLength={1024} required aria-invalid={!!passwordError} aria-describedby={passwordError ? "register-password-error" : undefined} value={form.password} onChange={(event) => { update("password", event.target.value); setPasswordError(""); }} /></label>
        {passwordError && <p id="register-password-error" className={styles.error} role="alert">{passwordError}</p>}
        <div className={styles.actions}><Button type="submit" disabled={busy}>{busy ? tr("Создаём аккаунт…", "Creating account…") : tr("Создать аккаунт", "Create account")}</Button></div>
        <p>{tr("Уже зарегистрированы?", "Already registered?")} <Link className={styles.link} to="/login" state={{ from: destination }}>{tr("Войти", "Sign in")}</Link></p>
      </form>
    </section>
  );
}
