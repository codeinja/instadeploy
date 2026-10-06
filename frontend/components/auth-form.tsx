"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Boxes, Globe, Loader2, Rocket, Server } from "lucide-react";
import { signIn, signUp } from "@/lib/auth-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ThemeToggle } from "@/components/theme-toggle";
import { BrandMark } from "@/components/app-sidebar";

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const isLogin = mode === "login";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    const { error } = isLogin
      ? await signIn.email({ email, password })
      : await signUp.email({ name, email, password });
    if (error) {
      setError(
        error.status === 429
          ? "Too many attempts. Please wait a minute and try again."
          : (error.message ?? "Something went wrong"),
      );
      setLoading(false);
      return;
    }
    // The first account is the admin: take them straight into the setup tour.
    let next = "/";
    try {
      const me = await fetch("/api/me").then((r) => r.json());
      if (me.is_admin && !me.setup_complete) next = "/setup";
    } catch {}
    router.push(next);
    router.refresh();
  }

  return (
    <div className="grid min-h-screen bg-background lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      {/* Brand panel */}
      <aside className="bg-brand relative hidden overflow-hidden p-10 text-white lg:flex lg:flex-col lg:justify-between">
        <div className="pointer-events-none absolute -top-32 -left-32 size-96 rounded-full bg-white/10 blur-3xl" />
        <div className="pointer-events-none absolute -right-24 -bottom-24 size-96 rounded-full bg-black/10 blur-3xl" />
        <div className="relative flex items-center gap-2.5 text-lg font-semibold">
          <span className="flex size-9 items-center justify-center rounded-lg bg-white/15 ring-1 ring-white/25 backdrop-blur">
            <Rocket className="size-4" />
          </span>
          Insta Deploy
        </div>
        <div className="relative space-y-8">
          <h2 className="max-w-md text-4xl leading-tight font-semibold tracking-tight text-balance">
            Deploy Docker. Get a URL.
          </h2>
          <ul className="space-y-4 text-white/85">
            {[
              {
                icon: Boxes,
                text: "Images, Dockerfiles, Compose projects or a docker run command",
              },
              {
                icon: Globe,
                text: "A public HTTPS address for every app, no open ports needed",
              },
              {
                icon: Server,
                text: "Runs on your own laptop, home server or VPS",
              },
            ].map(({ icon: Icon, text }) => (
              <li key={text} className="flex items-start gap-3">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-white/15">
                  <Icon className="size-4" />
                </span>
                <span className="pt-1 text-sm">{text}</span>
              </li>
            ))}
          </ul>
        </div>
        <p className="relative text-xs text-white/60">
          Self-hosted on your own machines.
        </p>
      </aside>

      {/* Form */}
      <main className="relative flex flex-col items-center justify-center px-4 py-12">
        <div className="absolute top-4 right-4">
          <ThemeToggle />
        </div>
        <div className="w-full max-w-sm space-y-8">
          <div className="space-y-2">
            <div className="mb-6 flex items-center gap-2.5 font-semibold lg:hidden">
              <BrandMark /> Insta Deploy
            </div>
            <h1 className="text-2xl font-semibold tracking-tight">
              {isLogin ? "Welcome back" : "Create your account"}
            </h1>
            <p className="text-sm text-muted-foreground">
              {isLogin
                ? "Sign in to manage your deployments."
                : "Self-hosted: this account lives on your own server."}
            </p>
          </div>
          <form onSubmit={submit} className="space-y-4">
            {!isLogin && (
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  required
                  autoFocus
                  autoComplete="name"
                  className="h-10"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
            )}
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                required
                autoFocus={isLogin}
                autoComplete="email"
                placeholder="you@example.com"
                className="h-10"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                required
                minLength={isLogin ? undefined : 8}
                autoComplete={isLogin ? "current-password" : "new-password"}
                className="h-10"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              {!isLogin && (
                <p className="text-xs text-muted-foreground">
                  At least 8 characters.
                </p>
              )}
            </div>
            {error && (
              <p
                role="alert"
                className="rounded-lg border border-destructive/25 bg-destructive/5 p-3 text-sm text-destructive"
              >
                {error}
              </p>
            )}
            <Button
              type="submit"
              size="lg"
              className="h-10 w-full"
              disabled={loading}
            >
              {loading && <Loader2 className="animate-spin" />}
              {isLogin ? "Sign in" : "Create account"}
            </Button>
          </form>
          <p className="text-center text-sm text-muted-foreground">
            {isLogin ? (
              <>
                No account?{" "}
                <Link
                  href="/register"
                  className="font-medium text-primary hover:underline"
                >
                  Create one
                </Link>
              </>
            ) : (
              <>
                Already have an account?{" "}
                <Link
                  href="/login"
                  className="font-medium text-primary hover:underline"
                >
                  Sign in
                </Link>
              </>
            )}
          </p>
        </div>
      </main>
    </div>
  );
}
