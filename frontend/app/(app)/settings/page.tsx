"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Laptop, Loader2, LogOut, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { authClient, useSession } from "@/lib/auth-client";
import { api, errorMessage } from "@/lib/api";
import type { Me, RegistryCredential } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { GitHubAppSetup } from "@/components/github-app-setup";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardFooter } from "@/components/ui/card";

type SessionRow = {
  id: string;
  token: string;
  userAgent?: string | null;
  ipAddress?: string | null;
  createdAt: Date;
  expiresAt: Date;
};

function describeAgent(ua?: string | null) {
  if (!ua) return "Unknown device";
  const browser = /Firefox/.test(ua)
    ? "Firefox"
    : /Edg\//.test(ua)
      ? "Edge"
      : /Chrome/.test(ua)
        ? "Chrome"
        : /Safari/.test(ua)
          ? "Safari"
          : "Browser";
  const os = /Windows/.test(ua)
    ? "Windows"
    : /Mac OS/.test(ua)
      ? "macOS"
      : /Android/.test(ua)
        ? "Android"
        : /iPhone|iPad/.test(ua)
          ? "iOS"
          : /Linux/.test(ua)
            ? "Linux"
            : "";
  return os ? `${browser} on ${os}` : browser;
}

export default function SettingsPage() {
  const { data: session, refetch } = useSession();
  const [me, setMe] = useState<Me | null>(null);
  const [name, setName] = useState("");
  const [savingName, setSavingName] = useState(false);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [savingPassword, setSavingPassword] = useState(false);
  const [sessions, setSessions] = useState<SessionRow[] | null>(null);

  const loadSessions = useCallback(async () => {
    const { data } = await authClient.listSessions();
    setSessions((data as SessionRow[] | null) ?? []);
  }, []);

  useEffect(() => {
    api<Me>("/me")
      .then(setMe)
      .catch((e) => toast.error(errorMessage(e)));
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch
    loadSessions();
  }, [loadSessions]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync form with loaded session
    if (session?.user.name) setName(session.user.name);
  }, [session?.user.name]);

  async function saveName(e: React.FormEvent) {
    e.preventDefault();
    setSavingName(true);
    const { error } = await authClient.updateUser({ name });
    setSavingName(false);
    if (error) return toast.error(error.message ?? "Could not update name");
    toast.success("Name updated");
    refetch();
  }

  async function changePassword(e: React.FormEvent) {
    e.preventDefault();
    setSavingPassword(true);
    const { error } = await authClient.changePassword({
      currentPassword,
      newPassword,
      revokeOtherSessions: true,
    });
    setSavingPassword(false);
    if (error) return toast.error(error.message ?? "Could not change password");
    setCurrentPassword("");
    setNewPassword("");
    toast.success("Password changed. Other sessions were signed out.");
    loadSessions();
  }

  async function revoke(token: string) {
    const { error } = await authClient.revokeSession({ token });
    if (error) return toast.error(error.message ?? "Could not revoke session");
    toast.success("Session revoked");
    loadSessions();
  }

  async function revokeOthers() {
    const { error } = await authClient.revokeOtherSessions();
    if (error) return toast.error(error.message ?? "Could not revoke sessions");
    toast.success("Signed out of all other sessions");
    loadSessions();
  }

  return (
    <div className="space-y-8">
      <PageHeader
        title="Settings"
        description="Your account and this Insta Deploy server."
      />

      <SettingsGroup
        title="Profile"
        description="How you appear in Insta Deploy."
      >
        <Card>
          <form onSubmit={saveName}>
            <CardContent className="space-y-4">
              <div className="flex items-center gap-3">
                <span className="bg-brand flex size-11 items-center justify-center rounded-full text-base font-semibold text-primary-foreground">
                  {(session?.user.name || session?.user.email || "?")
                    .slice(0, 1)
                    .toUpperCase()}
                </span>
                <div className="min-w-0">
                  <div className="truncate font-medium">
                    {session?.user.name ?? <Skeleton className="h-4 w-24" />}
                  </div>
                  <div className="truncate text-sm text-muted-foreground">
                    {session?.user.email ?? (
                      <Skeleton className="mt-1 h-4 w-40" />
                    )}
                  </div>
                </div>
              </div>
              <div className="max-w-sm space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
            </CardContent>
            <CardFooter className="mt-4 justify-end border-t bg-muted/30">
              <Button type="submit" disabled={savingName || !name}>
                {savingName && <Loader2 className="animate-spin" />} Save
              </Button>
            </CardFooter>
          </form>
        </Card>
      </SettingsGroup>

      <SettingsGroup
        title="Password"
        description="Changing your password signs you out everywhere else."
      >
        <Card>
          <form onSubmit={changePassword}>
            <CardContent className="grid max-w-sm gap-4">
              <div className="space-y-2">
                <Label htmlFor="current">Current password</Label>
                <Input
                  id="current"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="new">New password</Label>
                <Input
                  id="new"
                  type="password"
                  autoComplete="new-password"
                  required
                  minLength={8}
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  At least 8 characters.
                </p>
              </div>
            </CardContent>
            <CardFooter className="mt-4 justify-end border-t bg-muted/30">
              <Button type="submit" disabled={savingPassword}>
                {savingPassword && <Loader2 className="animate-spin" />} Change
                password
              </Button>
            </CardFooter>
          </form>
        </Card>
      </SettingsGroup>

      <SettingsGroup
        title="Sessions"
        description="Devices currently signed in to your account."
        actions={
          sessions &&
          sessions.length > 1 && (
            <Button variant="outline" size="sm" onClick={revokeOthers}>
              <LogOut /> Sign out other sessions
            </Button>
          )
        }
      >
        <Card className="gap-0 py-0">
          <CardContent className="divide-y p-0">
            {!sessions && <Skeleton className="m-4 h-10" />}
            {sessions?.map((s) => {
              const current = s.token === session?.session.token;
              return (
                <div
                  key={s.id}
                  className="flex items-center justify-between gap-4 px-4 py-3"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                      <Laptop className="size-4 text-muted-foreground" />
                    </span>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 text-sm font-medium">
                        {describeAgent(s.userAgent)}
                        {current && (
                          <Badge variant="secondary">This device</Badge>
                        )}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {s.ipAddress || "unknown IP"} · signed in{" "}
                        {new Date(s.createdAt).toLocaleString()}
                      </div>
                    </div>
                  </div>
                  {!current && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => revoke(s.token)}
                    >
                      Revoke
                    </Button>
                  )}
                </div>
              );
            })}
          </CardContent>
        </Card>
      </SettingsGroup>

      <SettingsGroup
        id="github"
        title="GitHub"
        description={
          <>
            Deploy private repositories and redeploy on every push. You create
            your own GitHub App, so Insta Deploy only gets read access to the
            repositories you choose.
          </>
        }
      >
        <GitHubAppSetup />
      </SettingsGroup>

      <SettingsGroup
        title="Private registries"
        description="Pull private images from Docker Hub, GHCR, GitLab or your own registry. Use an access token rather than your password."
      >
        <RegistriesCard />
      </SettingsGroup>

      <SettingsGroup
        title="Server"
        description={
          me?.is_admin
            ? "Server-wide settings. You can change them in the setup guide."
            : "Managed by the server's admin."
        }
      >
        <Card className="gap-0 py-0">
          <CardContent className="divide-y p-0">
            {!me ? (
              <Skeleton className="m-4 h-16" />
            ) : (
              <>
                <ServerRow
                  label="Server address for machines"
                  value={<span className="font-mono">{me.api_url}</span>}
                  action={
                    me.is_admin && (
                      <Link
                        href="/setup?step=address"
                        className={buttonVariants({
                          variant: "outline",
                          size: "sm",
                        })}
                      >
                        Change
                      </Link>
                    )
                  }
                />
                <ServerRow
                  label="Public URLs (Pangolin)"
                  value={
                    me.pangolin_enabled ? (
                      <span className="flex items-center gap-1.5">
                        <span className="size-1.5 rounded-full bg-success" />{" "}
                        Connected ·{" "}
                        <span className="font-mono">*.{me.apps_domain}</span>
                      </span>
                    ) : (
                      <span className="flex items-center gap-1.5 text-[oklch(0.55_0.13_70)] dark:text-warning">
                        <span className="size-1.5 rounded-full bg-warning" />{" "}
                        Not connected: apps run without public URLs
                      </span>
                    )
                  }
                  action={
                    me.is_admin && (
                      <Link
                        href="/setup?step=pangolin"
                        className={buttonVariants({
                          variant: me.pangolin_enabled ? "outline" : "default",
                          size: "sm",
                        })}
                      >
                        {me.pangolin_enabled ? "Change" : "Connect"}
                      </Link>
                    )
                  }
                />
              </>
            )}
          </CardContent>
        </Card>
      </SettingsGroup>
    </div>
  );
}

// A settings group: title and description on the left, the controls on the right.
function SettingsGroup({
  id,
  title,
  description,
  actions,
  children,
}: {
  id?: string;
  title: string;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section
      id={id}
      className="grid scroll-mt-20 gap-4 border-t pt-8 first-of-type:border-t-0 first-of-type:pt-0 md:grid-cols-[minmax(0,14rem)_minmax(0,1fr)] md:gap-8"
    >
      <div className="space-y-1">
        <h2 className="font-semibold">{title}</h2>
        {description && (
          <p className="text-sm text-muted-foreground">{description}</p>
        )}
        {actions && <div className="pt-2">{actions}</div>}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
}

function ServerRow({
  label,
  value,
  action,
}: {
  label: string;
  value: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-0.5">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        <div className="truncate text-sm">{value}</div>
      </div>
      {action}
    </div>
  );
}

// Credentials for private registries (Docker Hub, GHCR, GitLab, your own).
// Passwords are encrypted and never shown again.
function RegistriesCard() {
  const [list, setList] = useState<RegistryCredential[] | null>(null);
  const [server, setServer] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    api<RegistryCredential[]>("/registries")
      .then(setList)
      .catch((e) => toast.error(errorMessage(e)));
  }, []);
  useEffect(load, [load]);

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      await api("/registries", {
        method: "POST",
        body: { server, username, password },
      });
      toast.success(`Saved credentials for ${server}`);
      setServer("");
      setUsername("");
      setPassword("");
      load();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  async function remove(id: string) {
    await api(`/registries/${id}`, { method: "DELETE" }).catch((e) =>
      toast.error(errorMessage(e)),
    );
    load();
  }

  return (
    <Card>
      <CardContent className="space-y-4">
        {list?.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No registries saved. Public images work without one.
          </p>
        )}
        {list?.map((r) => (
          <div
            key={r.id}
            className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2 text-sm"
          >
            <span>
              <span className="font-mono">{r.server}</span>{" "}
              <span className="text-muted-foreground">as {r.username}</span>
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => remove(r.id)}
              aria-label={`Remove ${r.server}`}
            >
              <Trash2 />
            </Button>
          </div>
        ))}
        <form
          onSubmit={add}
          className="grid gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]"
        >
          <Input
            placeholder="ghcr.io"
            required
            value={server}
            onChange={(e) => setServer(e.target.value)}
            aria-label="Registry"
          />
          <Input
            placeholder="Username"
            required
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            aria-label="Username"
          />
          <Input
            placeholder="Password or token"
            type="password"
            required
            autoComplete="off"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-label="Password or token"
          />
          <Button type="submit" variant="outline" disabled={saving}>
            {saving ? <Loader2 className="animate-spin" /> : <Plus />} Add
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
