import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type { AuthConfig } from "@/lib/types";

interface AuthContextValue {
  token: string | null;
  authConfig: AuthConfig | null;
  loadingConfig: boolean;
  loginLocal: (email: string, password: string) => Promise<void>;
  setToken: (token: string) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue>(null!);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() =>
    localStorage.getItem("token")
  );
  const [authConfig, setAuthConfig] = useState<AuthConfig | null>(null);
  const [loadingConfig, setLoadingConfig] = useState(true);

  useEffect(() => {
    api
      .get<ApiEnvelope<AuthConfig>>("/auth/config")
      .then((res) => setAuthConfig(unwrap(res.data)))
      .catch(() => setAuthConfig({ mode: "local" }))
      .finally(() => setLoadingConfig(false));
  }, []);

  const setToken = useCallback((value: string) => {
    localStorage.setItem("token", value);
    setTokenState(value);
  }, []);

  const loginLocal = useCallback(
    async (email: string, password: string) => {
      const res = await api.post<ApiEnvelope<{ token: string }>>(
        "/users/login",
        { email, secret: password }
      );
      setToken(unwrap(res.data).token);
    },
    [setToken]
  );

  const logout = useCallback(() => {
    localStorage.removeItem("token");
    setTokenState(null);
  }, []);

  const value = useMemo(
    () => ({ token, authConfig, loadingConfig, loginLocal, setToken, logout }),
    [token, authConfig, loadingConfig, loginLocal, setToken, logout]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return useContext(AuthContext);
}
