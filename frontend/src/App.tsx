import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "@/lib/auth";
import { Layout } from "@/components/Layout";
import { LoginPage } from "@/pages/Login";
import { OIDCCallbackPage } from "@/pages/OIDCCallback";
import { HostsPage } from "@/pages/Hosts";
import type { ReactNode } from "react";

function RequireAuth({ children }: { children: ReactNode }) {
  const { token } = useAuth();
  if (!token) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/login/callback" element={<OIDCCallbackPage />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<HostsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default App;
