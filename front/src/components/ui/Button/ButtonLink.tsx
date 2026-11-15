import { Link, type LinkProps } from "react-router-dom";
import styles from "./Button.module.css";

interface ButtonLinkProps extends LinkProps {
  variant?: "primary" | "secondary" | "ghost";
}
export function ButtonLink({
  variant = "secondary",
  className,
  ...props
}: ButtonLinkProps) {
  return (
    <Link
      className={[styles.button, styles[variant], className]
        .filter(Boolean)
        .join(" ")}
      {...props}
    />
  );
}
