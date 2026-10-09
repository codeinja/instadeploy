"use client";

import { useCallback, useEffect, useState } from "react";
import {
  BookOpen,
  ChevronDown,
  ExternalLink,
  FileUp,
  FolderGit2,
  Loader2,
  Lock,
  RefreshCw,
  TriangleAlert,
  Webhook,
  WebhookOff,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { GitHubStatus } from "@/lib/types";
import { cn } from "@/lib/utils";
import { CopyButton } from "@/components/copy-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardFooter } from "@/components/ui/card";

function randomSecret() {
  const bytes = new Uint8Array(24);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// "minute", "5 minutes", "30 seconds": reads after "every" or "within a".
function describeInterval(seconds: number) {
  if (seconds === 60) return "minute";
  if (seconds % 60 === 0) return `${seconds / 60} minutes`;
  return `${seconds} seconds`;
}

// GitHub can only deliver webhooks to an address on the internet.
function isPrivateOrigin(origin: string) {
  try {
    const host = new URL(origin).hostname;
    return (
      host === "localhost" ||
      host.endsWith(".local") ||
      host.endsWith(".lan") ||
      host.endsWith(".internal") ||
      /^127\./.test(host) ||
      /^10\./.test(host) ||
      /^192\.168\./.test(host) ||
      /^172\.(1[6-9]|2\d|3[01])\./.test(host) ||
      host === "host.docker.internal" ||
      !host.includes(".")
    );
  } catch {
    return false;
  }
}

// Connects the user's own GitHub App: private repositories are read with
// short-lived, read-only tokens, and pushes trigger redeploys right away.
export function GitHubAppSetup() {
  const [status, setStatus] = useState<GitHubStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [showGuide, setShowGuide] = useState(false);
  const [appId, setAppId] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [secret, setSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [origin, setOrigin] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const s = await api<GitHubStatus>("/github");
      setStatus(s);
      if (!s.connected) {
        setShowGuide(true);
        setSecret((cur) => cur || randomSecret());
      }
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- read once on mount
    setOrigin(window.location.origin);
    load();
  }, [load]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      const s = await api<GitHubStatus>("/github", {
        method: "PUT",
        body: { app_id: appId, private_key: privateKey, webhook_secret: secret },
      });
      setStatus(s);
      setEditing(false);
      setShowGuide(false);
      setPrivateKey("");
      toast.success(`Connected ${s.name}`, {
        description:
          s.installations.length === 0
            ? "Now install it on the repositories you want to deploy."
            : `${s.repositories.length} repositories available.`,
      });
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  async function disconnect() {
    await api("/github", { method: "DELETE" });
    toast.success("GitHub App disconnected");
    setSecret(randomSecret());
    setAppId("");
    load();
  }

  if (loading && !status) {
    return (
      <Card>
        <CardContent>
          <Skeleton className="h-24" />
        </CardContent>
      </Card>
    );
  }
  if (!status) return null;

  const webhookURL = `${origin}${status.webhook_path}`;
  const privateDashboard = origin !== "" && isPrivateOrigin(origin);
  const pollEvery = describeInterval(status.poll_every_seconds);
  const showForm = !status.connected || editing;

  return (
    <div className="space-y-4">
      {status.connected && (
        <ConnectedCard
          status={status}
          pollEvery={pollEvery}
          loading={loading}
          onRefresh={load}
          onEdit={() => {
            setAppId(String(status.app_id ?? ""));
            setSecret("");
            setEditing((v) => !v);
          }}
          onDisconnect={() => setConfirmDisconnect(true)}
        />
      )}

      {showForm && (
        <>
          <Guide
            open={showGuide}
            onToggle={() => setShowGuide((v) => !v)}
            origin={origin}
            webhookURL={webhookURL}
            privateDashboard={privateDashboard}
            secret={secret}
            onNewSecret={() => setSecret(randomSecret())}
            pollEvery={pollEvery}
          />
          <Card>
            <form onSubmit={save}>
              <CardContent className="space-y-4">
                <div className="max-w-xs space-y-2">
                  <Label htmlFor="gh-app-id">App ID</Label>
                  <Input
                    id="gh-app-id"
                    inputMode="numeric"
                    required
                    placeholder="123456"
                    className="font-mono"
                    value={appId}
                    onChange={(e) => setAppId(e.target.value.trim())}
                  />
                </div>
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label htmlFor="gh-key">Private key</Label>
                    <label
                      className={buttonVariants({
                        variant: "outline",
                        size: "sm",
                      })}
                    >
                      <FileUp /> Open .pem file…
                      <input
                        type="file"
                        accept=".pem"
                        className="hidden"
                        onChange={async (e) => {
                          const f = e.target.files?.[0];
                          if (f) setPrivateKey(await f.text());
                        }}
                      />
                    </label>
                  </div>
                  <Textarea
                    id="gh-key"
                    rows={4}
                    required={!status.connected}
                    autoComplete="off"
                    spellCheck={false}
                    className="bg-muted/30 font-mono text-xs"
                    placeholder={
                      status.connected
                        ? "Leave empty to keep the saved key"
                        : "-----BEGIN RSA PRIVATE KEY-----\n…\n-----END RSA PRIVATE KEY-----"
                    }
                    value={privateKey}
                    onChange={(e) => setPrivateKey(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Encrypted on the server and never shown again.
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="gh-secret">
                    Webhook secret{" "}
                    <span className="font-normal text-muted-foreground">
                      (only if the webhook is active)
                    </span>
                  </Label>
                  <div className="flex gap-2">
                    <Input
                      id="gh-secret"
                      autoComplete="off"
                      spellCheck={false}
                      className="font-mono"
                      placeholder={
                        status.webhook_secret_set
                          ? "Leave empty to keep the saved secret"
                          : ""
                      }
                      value={secret}
                      onChange={(e) => setSecret(e.target.value.trim())}
                    />
                    {secret && <CopyButton value={secret} label="Copy secret" />}
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => setSecret(randomSecret())}
                    >
                      <RefreshCw /> Generate
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Must match the secret entered on GitHub exactly.
                  </p>
                </div>
              </CardContent>
              <CardFooter className="mt-4 justify-end gap-2 border-t bg-muted/30">
                {editing && (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setEditing(false)}
                  >
                    Cancel
                  </Button>
                )}
                <Button type="submit" disabled={saving || !appId}>
                  {saving && <Loader2 className="animate-spin" />} Check and
                  save
                </Button>
              </CardFooter>
            </form>
          </Card>
        </>
      )}

      <ConfirmDialog
        open={confirmDisconnect}
        onOpenChange={setConfirmDisconnect}
        title="Disconnect the GitHub App?"
        description="Deployments from private repositories will fail to build until an app is connected again. The app stays installed on GitHub; uninstall or delete it there if you no longer need it."
        confirmLabel="Disconnect"
        onConfirm={disconnect}
      />
    </div>
  );
}

function ConnectedCard({
  status,
  pollEvery,
  loading,
  onRefresh,
  onEdit,
  onDisconnect,
}: {
  status: GitHubStatus;
  pollEvery: string;
  loading: boolean;
  onRefresh: () => void;
  onEdit: () => void;
  onDisconnect: () => void;
}) {
  const [showRepos, setShowRepos] = useState(false);
  const repos = status.repositories;
  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted">
              <FolderGit2 className="size-5 text-muted-foreground" />
            </span>
            <div className="min-w-0">
              <div className="flex items-center gap-2 font-medium">
                <span className="size-1.5 rounded-full bg-success" />
                <a
                  href={status.html_url}
                  target="_blank"
                  rel="noreferrer"
                  className="truncate hover:underline"
                >
                  {status.name}
                </a>
              </div>
              <div className="text-xs text-muted-foreground">
                App ID <span className="font-mono">{status.app_id}</span>
                {status.owner && <> · owned by {status.owner}</>}
              </div>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onRefresh}
            disabled={loading}
            aria-label="Refresh"
          >
            <RefreshCw className={cn(loading && "animate-spin")} />
          </Button>
        </div>

        <div className="flex flex-wrap gap-2 text-xs">
          {status.webhook_secret_set ? (
            <Badge variant="secondary">
              <Webhook /> Push webhook: instant redeploys
            </Badge>
          ) : (
            <Badge variant="outline">
              <WebhookOff /> No webhook secret
            </Badge>
          )}
          <Badge variant="outline">
            <RefreshCw /> Checks for new commits every {pollEvery}
          </Badge>
        </div>

        {status.error && (
          <p className="flex gap-2 rounded-lg border border-destructive/25 bg-destructive/5 p-3 text-sm text-destructive">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            {status.error}
          </p>
        )}

        {!status.error && status.installations.length === 0 && (
          <div className="space-y-3 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
            <p>
              <b>One more step:</b> install the app on the repositories you want
              to deploy. Choose <b>Only select repositories</b> to keep access
              to a minimum.
            </p>
            <a
              href={status.install_url}
              target="_blank"
              rel="noreferrer"
              className={buttonVariants({ size: "sm" })}
            >
              Install on GitHub <ExternalLink />
            </a>
          </div>
        )}

        {status.installations.length > 0 && (
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">
              Installed on
            </p>
            {status.installations.map((i) => (
              <div
                key={i.id}
                className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm"
              >
                <span>
                  <span className="font-medium">{i.account}</span>{" "}
                  <span className="text-muted-foreground">
                    ·{" "}
                    {i.repository_selection === "all"
                      ? "all repositories"
                      : "selected repositories"}
                  </span>
                </span>
                <a
                  href={i.html_url}
                  target="_blank"
                  rel="noreferrer"
                  className={buttonVariants({ variant: "ghost", size: "sm" })}
                >
                  Choose repositories <ExternalLink />
                </a>
              </div>
            ))}
            <button
              type="button"
              onClick={() => setShowRepos((v) => !v)}
              className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
            >
              <ChevronDown
                className={cn(
                  "size-4 transition-transform",
                  !showRepos && "-rotate-90",
                )}
              />
              {repos.length} repositor{repos.length === 1 ? "y" : "ies"}{" "}
              available
              {status.repositories_truncated && " (first 1000)"}
            </button>
            {showRepos && (
              <div className="max-h-56 divide-y overflow-y-auto rounded-lg border text-sm">
                {repos.map((r) => (
                  <div
                    key={r.full_name}
                    className="flex items-center justify-between gap-3 px-3 py-1.5"
                  >
                    <span className="truncate font-mono text-xs">
                      {r.full_name}
                    </span>
                    {r.private && (
                      <Lock className="size-3.5 shrink-0 text-muted-foreground" />
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </CardContent>
      <CardFooter className="mt-4 flex-wrap justify-end gap-2 border-t bg-muted/30">
        <Button variant="ghost" onClick={onDisconnect}>
          Disconnect
        </Button>
        <Button variant="outline" onClick={onEdit}>
          Update credentials
        </Button>
        {status.installations.length > 0 && (
          <a
            href={status.install_url}
            target="_blank"
            rel="noreferrer"
            className={buttonVariants({ variant: "outline" })}
          >
            Install on another account <ExternalLink />
          </a>
        )}
      </CardFooter>
    </Card>
  );
}

function Guide({
  open,
  onToggle,
  origin,
  webhookURL,
  privateDashboard,
  secret,
  onNewSecret,
  pollEvery,
}: {
  open: boolean;
  onToggle: () => void;
  origin: string;
  webhookURL: string;
  privateDashboard: boolean;
  secret: string;
  onNewSecret: () => void;
  pollEvery: string;
}) {
  const steps: React.ReactNode[] = [
    <>
      Open{" "}
      <ExtLink href="https://github.com/settings/apps/new">
        GitHub → Settings → Developer settings → GitHub Apps → New GitHub App
      </ExtLink>
      . For repositories owned by an organization, create it in the
      organization instead:{" "}
      <code className="text-xs">
        github.com/organizations/&lt;org&gt;/settings/apps/new
      </code>
      .
    </>,
    <>
      <b>GitHub App name</b>: anything unique, for example{" "}
      <code className="text-xs">insta-deploy-yourname</code>.{" "}
      <b>Homepage URL</b>: this dashboard.
      <Value value={origin} />
    </>,
    privateDashboard ? (
      <>
        <b>Webhook</b>: this dashboard is at{" "}
        <code className="text-xs">{origin}</code>, which GitHub can&apos;t
        reach, so <b>uncheck Active</b>. New commits are still picked up by
        checking every {pollEvery}. (To get instant deploys later, open the
        dashboard from its public address and follow this step from there.)
      </>
    ) : (
      <>
        <b>Webhook</b>: keep <b>Active</b> checked and set the{" "}
        <b>Webhook URL</b> and <b>Secret</b> to:
        <Value value={webhookURL} />
        <Value value={secret} onRefresh={onNewSecret} />
        If GitHub can&apos;t reach this address, uncheck <b>Active</b> instead:
        new commits are still picked up every {pollEvery}.
      </>
    ),
    <>
      <b>Permissions → Repository permissions</b>: set <b>Contents</b> to{" "}
      <b>Read-only</b>. (Metadata: Read-only is added automatically.) Nothing
      else is needed.
    </>,
    <>
      <b>Subscribe to events</b>: check <b>Push</b>.
    </>,
    <>
      <b>Where can this GitHub App be installed?</b> Choose{" "}
      <b>Only on this account</b>, then click <b>Create GitHub App</b>.
    </>,
    <>
      On the app&apos;s page, copy the <b>App ID</b>. Scroll down to{" "}
      <b>Private keys</b> and click <b>Generate a private key</b>: a{" "}
      <code className="text-xs">.pem</code> file downloads.
    </>,
    <>
      Enter the App ID and the .pem file below
      {privateDashboard ? "" : " (the webhook secret is already filled in)"}{" "}
      and click <b>Check and save</b>.
    </>,
    <>
      Click <b>Install on GitHub</b> (shown after saving), choose{" "}
      <b>Only select repositories</b> and pick the repositories to deploy.
      They then appear in the repository list when you create a Dockerfile or
      Compose deployment. Turn on <b>Auto deploy</b> to redeploy on every
      commit.
    </>,
  ];
  return (
    <div className="rounded-xl border bg-muted/20">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-medium"
      >
        <BookOpen className="size-4 text-muted-foreground" />
        <span className="flex-1">
          How to create a GitHub App for private repositories
        </span>
        <ChevronDown
          className={cn("size-4 transition-transform", !open && "-rotate-90")}
        />
      </button>
      {open && (
        <ol className="space-y-3 border-t px-4 py-4 text-sm">
          {steps.map((step, i) => (
            <li key={i} className="flex gap-3">
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-primary text-[11px] font-semibold text-primary-foreground">
                {i + 1}
              </span>
              <div className="min-w-0 flex-1 space-y-1.5 leading-relaxed">
                {step}
              </div>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function ExtLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="font-medium text-primary underline-offset-2 hover:underline"
    >
      {children}
      <ExternalLink className="ml-0.5 inline size-3 align-baseline" />
    </a>
  );
}

function Value({
  value,
  onRefresh,
}: {
  value: string;
  onRefresh?: () => void;
}) {
  return (
    <div className="flex items-center gap-1 rounded-md border bg-background py-0.5 pr-1 pl-2.5">
      <code className="min-w-0 flex-1 truncate font-mono text-xs">
        {value || "…"}
      </code>
      {onRefresh && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onRefresh}
          aria-label="Generate a new secret"
          title="Generate a new secret"
        >
          <RefreshCw />
        </Button>
      )}
      <CopyButton value={value} />
    </div>
  );
}
