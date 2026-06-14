import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/lib/auth";

// The backend redirects here with the CPM token in the URL fragment
// (#token=...). We store it and move into the app.
export function OIDCCallbackPage() {
  const { setToken } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    const hash = window.location.hash.replace(/^#/, "");
    const token = new URLSearchParams(hash).get("token");
    if (token) {
      setToken(token);
      navigate("/", { replace: true });
    } else {
      navigate("/login?error=login%20failed", { replace: true });
    }
  }, [setToken, navigate]);

  return (
    <div className="flex min-h-screen items-center justify-center text-muted-foreground">
      Signing you in…
    </div>
  );
}
