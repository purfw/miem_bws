import { Navigate, useLocation } from "react-router-dom";
import { tr } from "../i18n/runtime";
import { useAuth } from "./useAuth";
import type { ReactNode } from "react";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();
  if (loading) return <p style={{ padding: "48px 0" }}>{tr("Проверяем сессию…", "Checking session…")}</p>;
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return children;
}
