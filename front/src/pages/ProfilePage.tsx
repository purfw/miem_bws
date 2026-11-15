import {
  Link,
  Navigate,
  NavLink,
  useLocation,
  useNavigate,
  useParams,
} from "react-router-dom";
import { useState } from "react";
import { useAuth } from "../auth/useAuth";
import { readableError } from "../api/client";
import { Button } from "../components/ui/Button/Button";
import { tr } from "../i18n/runtime";
import { ProfilePersonal } from "./profile/ProfilePersonal";
import { ProfileApplications } from "./profile/ProfileApplications";
import { ProfileFavorites } from "./profile/ProfileFavorites";
import { ProfileNotifications } from "./profile/ProfileNotifications";
import { ProfileDocuments } from "./profile/ProfileDocuments";
import { ProfileOrganizer } from "./profile/ProfileOrganizer";
import { ProfileSecurity } from "./profile/ProfileSecurity";
import page from "./Page.module.css";
import styles from "./profile/Profile.module.css";

export function ProfilePage() {
  const { user, logout } = useAuth();
  const { section } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const sections = [
    {
      id: "personal",
      title: tr("Личные данные", "Personal details"),
      component: ProfilePersonal,
    },
    {
      id: "applications",
      title: tr("Мои заявки", "My applications"),
      component: ProfileApplications,
    },
    {
      id: "favorites",
      title: tr("Избранное", "Favorites"),
      component: ProfileFavorites,
    },
    {
      id: "notifications",
      title: tr("Уведомления", "Notifications"),
      component: ProfileNotifications,
    },
    {
      id: "documents",
      title: tr("Мои документы", "My documents"),
      component: ProfileDocuments,
    },
    {
      id: "organizer",
      title: tr("Полномочия организатора", "Organizer permissions"),
      component: ProfileOrganizer,
    },
    {
      id: "security",
      title: tr("Безопасность", "Security"),
      component: ProfileSecurity,
    },
  ];
  const selected = sections.find((item) => item.id === section);
  const state = location.state as {
    from?: string;
    verifyEmail?: boolean;
  } | null;
  const eventPath = state?.from?.startsWith("/conferences/")
    ? state.from
    : null;
  if (!selected)
    return (
      <Navigate
        to={state?.verifyEmail ? "/profile/security" : "/profile/personal"}
        replace
        state={location.state}
      />
    );
  const Content = selected.component;
  async function signOut() {
    setBusy(true);
    try {
      await logout();
      navigate("/");
    } catch (reason) {
      setError(readableError(reason));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className={`${page.page} ${styles.page}`}>
      <header className={page.profileHeader}>
        <div>
          <span className={page.eyebrow}>
            {tr("Личный кабинет", "Personal account")}
          </span>
          <h1 className={page.title}>
            {user?.first_name} {user?.last_name}
          </h1>
          <p className={`${page.subtitle} ${styles.wrap}`}>
            {user?.email}
            {user?.affiliation ? ` · ${user.affiliation}` : ""}
          </p>
        </div>
        <Button variant="ghost" disabled={busy} onClick={signOut}>
          {tr("Выйти", "Sign out")}
        </Button>
      </header>
      {error && (
        <p className={page.error} role="alert">
          {error}
        </p>
      )}
      {eventPath && (
        <Link className={page.link} to={eventPath}>
          {tr("Вернуться к мероприятию", "Return to the event")}
        </Link>
      )}
      <div className={styles.layout}>
        <nav
          className={styles.navigation}
          aria-label={tr("Разделы профиля", "Profile sections")}
        >
          {sections.map((item) => (
            <NavLink
              key={item.id}
              to={`/profile/${item.id}`}
              state={location.state}
              className={({ isActive }) =>
                `${styles.navItem} ${isActive ? styles.navActive : ""}`
              }
            >
              {item.title}
            </NavLink>
          ))}
        </nav>
        <label className={`${page.field} ${styles.mobileNavigation}`}>
          {tr("Раздел профиля", "Profile section")}
          <select
            value={selected.id}
            onChange={(event) =>
              navigate(`/profile/${event.target.value}`, {
                state: location.state,
              })
            }
          >
            {sections.map((item) => (
              <option key={item.id} value={item.id}>
                {item.title}
              </option>
            ))}
          </select>
        </label>
        <div className={styles.content} key={`${user?.id}:${selected.id}`}>
          <h2 className={styles.heading}>{selected.title}</h2>
          <Content />
        </div>
      </div>
    </section>
  );
}
