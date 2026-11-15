import { createContext } from "react";
import type { ApiRequestOptions } from "../api/client";
import type { User } from "../types/conference";

export interface RegisterInput {
  email: string;
  password: string;
  first_name: string;
  last_name: string;
  patronymic?: string;
  affiliation?: string;
	locale?: "ru" | "en";
}

export interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<User>;
  register: (input: RegisterInput) => Promise<void>;
  logout: () => Promise<void>;
  refreshSession: () => Promise<User>;
  updateUser: (user: User | null) => void;
  request: <T>(path: string, options?: ApiRequestOptions) => Promise<T>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);
