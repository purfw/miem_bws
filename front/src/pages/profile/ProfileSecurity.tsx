import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../../auth/useAuth";
import { ApiError, passwordFieldError, readableError } from "../../api/client";
import { Button } from "../../components/ui/Button/Button";
import { tr } from "../../i18n/runtime";
import { ProfileBadge, ProfileFeedback } from "./ProfileUI";
import page from "../Page.module.css";
import styles from "./Profile.module.css";

export function ProfileSecurity() {
  const { user, request, logout, refreshSession } = useAuth();
  const navigate = useNavigate();
  const cooldownKey = `confhub-verification-cooldown:${user?.id}`;
  const [until, setUntil] = useState(() => {
    try {
      return Number(localStorage.getItem(cooldownKey)) || 0;
    } catch {
      return 0;
    }
  });
  const [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [passwords, setPasswords] = useState({ current: "", next: "" });
  const [passwordError, setPasswordError] = useState("");
  const [deletePassword, setDeletePassword] = useState("");
  const remaining = Math.max(0, Math.ceil((until - now) / 1000));
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    function sync(event: StorageEvent) {
      if (event.key === cooldownKey) setUntil(Number(event.newValue) || 0);
    }
    window.addEventListener("storage", sync);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("storage", sync);
    };
  }, [cooldownKey]);
  function cooldown(seconds: number) {
    const timestamp = Date.now();
    const deadline = timestamp + seconds * 1000;
    setNow(timestamp);
    setUntil(deadline);
    try {
      localStorage.setItem(cooldownKey, String(deadline));
    } catch {
      return;
    }
  }
  async function resend() {
    if (busy || remaining || user?.email_verified) return;
    setBusy("verify");
    setError("");
    setMessage("");
    try {
      await request<void>("/auth/resend-verification", { method: "POST" });
      cooldown(60);
      setMessage(
        tr(
          "Письмо подтверждения поставлено в очередь отправки.",
          "The verification email has been queued.",
        ),
      );
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 429)
        cooldown(reason.retryAfterSeconds ?? 60);
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function checkEmail() {
    setBusy("verify");
    setError("");
    setMessage("");
    try {
      const updated = await refreshSession();
      setMessage(
        updated.email_verified
          ? tr(
              "Email подтверждён. Можно регистрироваться на мероприятия.",
              "Email verified. You can register for events.",
            )
          : tr(
              "Email ещё не подтверждён. Откройте ссылку из письма. Если письмо не приходит, обратитесь в поддержку.",
              "Email is not verified yet. Open the link in the email. If it does not arrive, contact support.",
            ),
      );
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function changePassword(event: FormEvent) {
    event.preventDefault();
    setBusy("password");
    setError("");
    setMessage("");
    setPasswordError("");
    try {
      await request<void>("/auth/me/password", {
        method: "PUT",
        body: {
          current_password: passwords.current,
          new_password: passwords.next,
        },
      });
      await logout();
      navigate("/login", { replace: true });
    } catch (reason) {
      const fieldError = passwordFieldError(reason);
      if (fieldError) setPasswordError(fieldError);
      else setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  async function deleteAccount(event: FormEvent) {
    event.preventDefault();
    if (
      !window.confirm(
        tr(
          "Удалить аккаунт? Персональные данные будут обезличены, восстановить аккаунт автоматически нельзя.",
          "Delete the account? Personal data will be anonymized and the account cannot be restored automatically.",
        ),
      )
    )
      return;
    setBusy("delete");
    setError("");
    try {
      await request<void>("/auth/me", {
        method: "DELETE",
        body: { current_password: deletePassword },
      });
      // The account is already deleted; logout() clears local state even if the request fails.
      await logout().catch(() => undefined);
      navigate("/", { replace: true });
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy("");
    }
  }
  return (
    <>
      <ProfileFeedback error={error} message={message} />
      <section
        className={`${styles.panel} ${styles.stack}`}
        aria-labelledby="email-security-heading"
      >
        <div className={styles.itemHeader}>
          <h3 id="email-security-heading">Email</h3>
          <ProfileBadge tone={user?.email_verified ? "accepted" : "active"}>
            {user?.email_verified
              ? tr("Подтверждён", "Verified")
              : tr("Не подтверждён", "Not verified")}
          </ProfileBadge>
        </div>
        <p className={styles.wrap}>{user?.email}</p>
        {!user?.email_verified && (
          <>
            <p className={styles.muted}>
              {tr(
                "Подтвердите email для регистрации слушателем. Письмо будет отправлено только на адрес вашего аккаунта.",
                "Verify your email to register as an attendee. Messages are sent only to your account email.",
              )}
            </p>
            <div className={styles.actions}>
              <Button
                variant="secondary"
                disabled={Boolean(busy) || remaining > 0}
                onClick={resend}
                aria-describedby="verification-cooldown"
              >
                {remaining > 0
                  ? `${tr("Повторить через", "Retry in")} ${remaining} ${tr("с", "s")}`
                  : tr("Отправить повторно", "Resend email")}
              </Button>
              <Button
                variant="ghost"
                disabled={Boolean(busy)}
                onClick={checkEmail}
              >
                {tr("Проверить email", "Check verification")}
              </Button>
            </div>
            <p id="verification-cooldown" className={styles.muted}>
              {tr(
                "Не чаще одного письма в минуту и трёх писем в час.",
                "At most one email per minute and three per hour.",
              )}{" "}
              <Link className={page.link} to="/support">
                {tr("Письмо не приходит", "Email has not arrived")}
              </Link>
            </p>
          </>
        )}
      </section>
      <form
        className={`${styles.panel} ${styles.form}`}
        onSubmit={changePassword}
      >
        <h3>{tr("Изменить пароль", "Change password")}</h3>
        <label className={page.field}>
          {tr("Текущий пароль", "Current password")}
          <input
            type="password"
            autoComplete="current-password"
            required
            value={passwords.current}
            onChange={(event) =>
              setPasswords({ ...passwords, current: event.target.value })
            }
          />
        </label>
        <label className={page.field}>
          {tr(
            "Новый пароль (не менее 12 символов)",
            "New password (at least 12 characters)",
          )}
          <input
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={1024}
            aria-invalid={Boolean(passwordError)}
            aria-describedby={
              passwordError ? "profile-password-error" : undefined
            }
            value={passwords.next}
            onChange={(event) => {
              setPasswords({ ...passwords, next: event.target.value });
              setPasswordError("");
            }}
          />
        </label>
        {passwordError && (
          <p id="profile-password-error" className={page.error} role="alert">
            {passwordError}
          </p>
        )}
        <Button type="submit" disabled={Boolean(busy)}>
          {tr("Изменить пароль", "Change password")}
        </Button>
      </form>
      <form
        className={`${styles.panel} ${styles.form} ${styles.danger}`}
        onSubmit={deleteAccount}
      >
        <h3>{tr("Удаление аккаунта", "Delete account")}</h3>
        <p className={styles.muted}>
          {tr(
            "Персональные данные будут обезличены. История участия сохранится без персональных идентификаторов для целостности журнала.",
            "Personal data will be anonymized. Participation history is retained without personal identifiers to preserve audit integrity.",
          )}
        </p>
        <label className={page.field}>
          {tr(
            "Подтвердите текущим паролем",
            "Confirm with your current password",
          )}
          <input
            type="password"
            autoComplete="current-password"
            required
            value={deletePassword}
            onChange={(event) => setDeletePassword(event.target.value)}
          />
        </label>
        <Button variant="secondary" type="submit" disabled={Boolean(busy)}>
          {tr("Удалить аккаунт", "Delete account")}
        </Button>
      </form>
    </>
  );
}
