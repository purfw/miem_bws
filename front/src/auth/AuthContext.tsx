import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ApiError, apiRequest, readableError, type ApiRequestOptions } from "../api/client";
import type { User } from "../types/conference";
import { AuthContext, type RegisterInput } from "./context";

interface AuthSession { access_token: string; expires_at: string; user: User }
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [sessionError, setSessionError] = useState("");
  const [sessionAttempt, setSessionAttempt] = useState(0);
  const tokenRef = useRef<string | null>(null);
  const refreshPromise = useRef<Promise<AuthSession | null> | null>(null);
  // Bumped by login/logout so that a refresh started earlier cannot overwrite the newer session state.
  const generation = useRef(0);

  const refresh = useCallback(async (): Promise<AuthSession | null> => {
    if (refreshPromise.current) return refreshPromise.current;
    const startedAt = generation.current;
    refreshPromise.current = apiRequest<AuthSession>("/auth/refresh", { method: "POST" })
      .then((session) => {
        if (generation.current !== startedAt) return null;
        tokenRef.current = session.access_token;
        setUser(session.user);
        return session;
      })
      .catch((error: unknown) => {
        if (generation.current !== startedAt) return null;
        if (!(error instanceof ApiError) || (error.status !== 401 && error.status !== 403)) throw error;
        tokenRef.current = null;
        setUser(null);
        return null;
      })
      .finally(() => { refreshPromise.current = null; });
    return refreshPromise.current;
  }, []);

  useEffect(() => {
    let active = true;
    refresh()
      .then(() => { if (active) setSessionError(""); })
      // Network/5xx failure: the session state is unknown, not "logged out".
      .catch((error: unknown) => { if (active) setSessionError(readableError(error)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [refresh, sessionAttempt]);

  const retrySession = useCallback(() => {
    setLoading(true);
    setSessionError("");
    setSessionAttempt((value) => value + 1);
  }, []);

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
    generation.current += 1;
    tokenRef.current = session.access_token;
    setUser(session.user);
    setSessionError("");
    return session.user;
  }, []);

  const register = useCallback(async (input: RegisterInput) => {
    await apiRequest<{ user_id: string }>("/auth/register", { method: "POST", body: input });
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiRequest<void>("/auth/logout", { method: "POST" });
    } finally {
      generation.current += 1;
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
    () => ({ user, loading, sessionError, retrySession, login, register, logout, refreshSession, updateUser: setUser, request }),
    [user, loading, sessionError, retrySession, login, register, logout, refreshSession, request],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
