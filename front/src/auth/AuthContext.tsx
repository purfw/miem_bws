import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ApiError, apiRequest, type ApiRequestOptions } from "../api/client";
import type { User } from "../types/conference";
import { AuthContext, type RegisterInput } from "./context";

interface AuthSession { access_token: string; expires_at: string; user: User }
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const tokenRef = useRef<string | null>(null);
  const refreshPromise = useRef<Promise<AuthSession | null> | null>(null);

  const refresh = useCallback(async (): Promise<AuthSession | null> => {
    if (refreshPromise.current) return refreshPromise.current;
    refreshPromise.current = apiRequest<AuthSession>("/auth/refresh", { method: "POST" })
      .then((session) => {
        tokenRef.current = session.access_token;
        setUser(session.user);
        return session;
      })
      .catch((error: unknown) => {
        if (!(error instanceof ApiError) || (error.status !== 401 && error.status !== 403)) throw error;
        tokenRef.current = null;
        setUser(null);
        return null;
      })
      .finally(() => { refreshPromise.current = null; });
    return refreshPromise.current;
  }, []);

  useEffect(() => { void refresh().catch(() => undefined).finally(() => setLoading(false)); }, [refresh]);

  const refreshSession = useCallback(async () => {
    const session = await refresh();
    if (!session) throw new ApiError(401);
    return session.user;
  }, [refresh]);

  const login = useCallback(async (email: string, password: string) => {
    const session = await apiRequest<AuthSession>("/auth/login", {
      method: "POST",
      body: { email, password },
    });
    tokenRef.current = session.access_token;
    setUser(session.user);
    return session.user;
  }, []);

  const register = useCallback(async (input: RegisterInput) => {
    await apiRequest<{ user_id: string }>("/auth/register", { method: "POST", body: input });
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiRequest<void>("/auth/logout", { method: "POST" });
    } finally {
      tokenRef.current = null;
      setUser(null);
    }
  }, []);

  const request = useCallback(async <T,>(path: string, options: ApiRequestOptions = {}): Promise<T> => {
    try {
      return await apiRequest<T>(path, { ...options, token: tokenRef.current });
    } catch (error) {
      const protectedAuth = path.startsWith("/auth/me") || path === "/auth/resend-verification";
      if (!(error instanceof ApiError) || error.status !== 401 || (path.startsWith("/auth/") && !protectedAuth)) throw error;
      const session = await refresh();
      if (!session) throw error;
      return apiRequest<T>(path, { ...options, token: session.access_token });
    }
  }, [refresh]);

  const value = useMemo(
    () => ({ user, loading, login, register, logout, refreshSession, updateUser: setUser, request }),
    [user, loading, login, register, logout, refreshSession, request],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
