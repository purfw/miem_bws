import { Navigate, useLocation } from "react-router-dom";
import { tr } from "../i18n/runtime";
import { useAuth } from "./useAuth";
import { Button } from "../components/ui/Button/Button";
import type { ReactNode } from "react";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading, sessionError, retrySession } = useAuth();
  const location = useLocation();
  if (loading) return <p style={{ padding: "48px 0" }}>{tr("Проверяем сессию…", "Checking session…")}</p>;
  if (!user && sessionError) {
    return (
      <div style={{ padding: "48px 0" }} role="alert">
        <p>{tr("Не удалось проверить сессию:", "Could not check your session:")} {sessionError}</p>
        <Button type="button" onClick={retrySession}>{tr("Повторить", "Retry")}</Button>
      </div>
    );
  }
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return children;
}
