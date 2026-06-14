import { useState } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { KeyRound, LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAuth } from "@/lib/auth";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});

type FormValues = z.infer<typeof schema>;

export function LoginPage() {
  const { token, authConfig, loadingConfig, loginLocal } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [submitting, setSubmitting] = useState(false);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });

  if (token) {
    return <Navigate to="/" replace />;
  }

  const error = params.get("error");
  if (error) {
    toast.error(error);
  }

  const onSubmit = handleSubmit(async (data) => {
    setSubmitting(true);
    try {
      await loginLocal(data.email, data.password);
      navigate("/");
    } catch {
      toast.error("Login incorrect");
    } finally {
      setSubmitting(false);
    }
  });

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>Access your Caddy Proxy Manager</CardDescription>
        </CardHeader>
        <CardContent>
          {loadingConfig ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : authConfig?.mode === "oidc" ? (
            <Button
              className="w-full"
              onClick={() => {
                window.location.href = "/api/auth/oidc/login";
              }}
            >
              <KeyRound className="size-4" />
              Login with SSO
            </Button>
          ) : (
            <form className="space-y-4" onSubmit={onSubmit}>
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  type="email"
                  placeholder="admin@example.com"
                  {...register("email")}
                />
                {errors.email && (
                  <p className="text-sm text-destructive">
                    Enter a valid email
                  </p>
                )}
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input id="password" type="password" {...register("password")} />
                {errors.password && (
                  <p className="text-sm text-destructive">
                    Password is required
                  </p>
                )}
              </div>
              <Button type="submit" className="w-full" disabled={submitting}>
                <LogIn className="size-4" />
                {submitting ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
