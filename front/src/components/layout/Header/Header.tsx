import {
  NavLink,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import styles from "./Header.module.css";
import icon from "../../../assets/icons/logo.svg";
import searchIcon from "../../../assets/icons/search.svg";
import arrowDown from "../../../assets/icons/arrow-down.svg";
import { useAuth } from "../../../auth/useAuth";
import { useEffect, useState } from "react";
import { apiRequest } from "../../../api/client";
import { tr } from "../../../i18n/runtime";
import { useLocale } from "../../../i18n/useLocale";

export function Header() {
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const { user } = useAuth();
  const { locale, setLocale } = useLocale();
  const query = params.get("q") ?? "";
  const [suggestions, setSuggestions] = useState<string[]>([]);

  useEffect(() => {
    if (query.trim().length < 2) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      apiRequest<{ data: { suggestions: Array<{ text: string }> } }>(`/search/autocomplete?q=${encodeURIComponent(query.trim())}&limit=5`, { signal: controller.signal })
        .then((result) => setSuggestions(result.data.suggestions.map((item) => item.text)))
        .catch(() => undefined);
    }, 200);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [query]);

  function goToCatalog(value: string) {
    const trimmed = value.trim();
    const target = trimmed
      ? `/conferences?q=${encodeURIComponent(trimmed)}`
      : "/conferences";
    // Уже на каталоге — обновляем URL без новой записи в истории.
    navigate(target, { replace: location.pathname === "/conferences" });
  }

  function handleChange(event: React.ChangeEvent<HTMLInputElement>) {
    const value = event.target.value;
    goToCatalog(value);
  }

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    goToCatalog(query);
  }

  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <NavLink to="/" className={styles.logo}>
          <img src={icon} alt="ConferenceHub" className={styles.logoIcon} />
          <span className={styles.logoText}>ConferenceHub</span>
        </NavLink>

        <form className={styles.search} onSubmit={handleSubmit} role="search">
          <img
            src={searchIcon}
            alt=""
            aria-hidden="true"
            className={styles.searchIcon}
          />
          <input
            type="search"
            className={styles.searchInput}
            placeholder={tr("Поиск конференций, авторов, тем…", "Search conferences, authors, topics…")}
            aria-label={tr("Поиск мероприятий", "Search events")}
            value={query}
            onChange={handleChange}
            list="conference-search-suggestions"
          />
          <datalist id="conference-search-suggestions">{suggestions.map((suggestion) => <option key={suggestion} value={suggestion} />)}</datalist>
        </form>

        <nav className={styles.nav} aria-label={tr("Основная навигация", "Main navigation")}>
          <NavLink
            to="/conferences"
            className={({ isActive }) =>
              isActive
                ? `${styles.navLink} ${styles.navLinkActive}`
                : styles.navLink
            }
          >
            {tr("Конференции", "Conferences")}
          </NavLink>
          <NavLink
            to="/about"
            className={({ isActive }) =>
              isActive
                ? `${styles.navLink} ${styles.navLinkActive}`
                : styles.navLink
            }
          >
            {tr("О нас", "About")}
          </NavLink>
          <NavLink
            to="/support"
            className={({ isActive }) =>
              isActive
                ? `${styles.navLink} ${styles.navLinkActive}`
                : styles.navLink
            }
          >
            {tr("Поддержка", "Support")}
          </NavLink>
          {user && ["verified_organizer", "organizer_member", "admin"].includes(user.role) && <NavLink
            to="/organizer"
            className={({ isActive }) => isActive ? `${styles.navLink} ${styles.navLinkActive}` : styles.navLink}
          >{tr("Организатору", "Organizer")}</NavLink>}
          <NavLink
            to="/docs"
            className={({ isActive }) =>
              isActive
                ? `${styles.navLink} ${styles.navLinkActive}`
                : styles.navLink
            }
            aria-label={tr("Документация API", "API documentation")}
          >
            API
          </NavLink>
        </nav>

        <div className={styles.profile}>
          <div className={styles.locale} role="group" aria-label={tr("Язык интерфейса", "Interface language")}>
            <button type="button" className={locale === "ru" ? styles.localeActive : ""} aria-pressed={locale === "ru"} onClick={() => setLocale("ru")}>RU</button>
            <button type="button" className={locale === "en" ? styles.localeActive : ""} aria-pressed={locale === "en"} onClick={() => setLocale("en")}>EN</button>
          </div>
          <button
            className={styles.profileButton}
            type="button"
            aria-label={user ? tr("Открыть профиль", "Open profile") : tr("Войти", "Sign in")}
            onClick={() => navigate(user ? "/profile" : "/login", { state: { from: location.pathname } })}
          >
            <span className={styles.avatar}>
              {user ? `${user.first_name[0] ?? ""}${user.last_name[0] ?? ""}`.toUpperCase() : tr("ВХ", "IN")}
            </span>
            <img
              src={arrowDown}
              alt=""
              aria-hidden="true"
              className={styles.avatarArrow}
            />
          </button>
        </div>
      </div>
    </header>
  );
}
