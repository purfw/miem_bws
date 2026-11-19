import styles from "./Footer.module.css";
import icon from "../../../assets/icons/logo.svg";

export function Footer() {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        <div className={styles.brand}>
          <img src={icon} alt="ConferenceHub" className={styles.logoMark} />
          <span>ConferenceHub</span>
        </div>
        <div className={styles.meta}>
          <span>© {new Date().getFullYear()} ConferenceHub</span>
          <span className={styles.version}>v {__APP_VERSION__}</span>
        </div>
      </div>
    </footer>
  );
}
