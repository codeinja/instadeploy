"use client";

import { useState } from "react";
import {
  CheckCircle2,
  Cloud,
  ExternalLink,
  Loader2,
  Server,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { PangolinDomain, ServerSettings } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { PangolinLimits } from "@/components/pangolin-limits";
import { Badge } from "@/components/ui/badge";

const CLOUD = {
  apiUrl: "https://api.pangolin.net/v1",
  endpoint: "https://app.pangolin.net",
};

const permissions = [
  ["Site", "create, get, delete"],
  ["Resource", "create, get, update, delete"],
  ["Target", "create, list, update"],
  ["Domain", "list, create, get, delete, restart"],
];

// Guided Pangolin connection: explains what to do in Pangolin, checks the
// connection, and lets the admin pick the apps domain from a list.
export function PangolinSetup({
  settings,
  onSaved,
}: {
  settings: ServerSettings;
  onSaved: (s: ServerSettings) => void;
}) {
  const isCloud =
    !settings.pangolin_api_url || settings.pangolin_api_url === CLOUD.apiUrl;
  const [mode, setMode] = useState<"cloud" | "self">(
    isCloud ? "cloud" : "self",
  );
  const [orgId, setOrgId] = useState(settings.pangolin_org_id);
  const [apiKey, setApiKey] = useState("");
  const [apiUrl, setApiUrl] = useState(
    isCloud ? "" : settings.pangolin_api_url,
  );
  const [endpoint, setEndpoint] = useState(
    isCloud ? "" : settings.pangolin_endpoint,
  );
  const [domains, setDomains] = useState<PangolinDomain[] | null>(null);
  const [domain, setDomain] = useState(settings.apps_domain);
  const [busy, setBusy] = useState<"test" | "save" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const input = () => ({
    api_url: mode === "cloud" ? CLOUD.apiUrl : apiUrl.trim(),
    endpoint: mode === "cloud" ? CLOUD.endpoint : endpoint.trim(),
    org_id: orgId.trim(),
    api_key: apiKey.trim(),
    apps_domain: domain,
  });

  async function test() {
    setBusy("test");
    setError(null);
    setDomains(null);
    try {
      const res = await api<{ domains: PangolinDomain[] }>(
        "/settings/pangolin/test",
        { method: "POST", body: input() },
      );
      setDomains(res.domains);
      const verified = res.domains.filter((d) => d.verified);
      if (
        !res.domains.some((d) => d.domain === domain) &&
        verified.length === 1
      )
        setDomain(verified[0].domain);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  async function save() {
    setBusy("save");
    setError(null);
    try {
      const s = await api<ServerSettings>("/settings", {
        method: "PUT",
        body: { pangolin: input() },
      });
      toast.success("Pangolin connected", {
        description: `Apps get URLs under ${s.apps_domain}`,
      });
      onSaved(s);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  const canTest =
    orgId.trim() &&
    (apiKey.trim() || settings.pangolin_key_set) &&
    (mode === "cloud" || (apiUrl && endpoint));

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-2">
        <ChoiceCard
          active={mode === "cloud"}
          onClick={() => setMode("cloud")}
          icon={Cloud}
          title="Pangolin Cloud"
          text="Hosted by the Pangolin team. Easiest: nothing to install."
        />
        <ChoiceCard
          active={mode === "self"}
          onClick={() => setMode("self")}
          icon={Server}
          title="Self-hosted Pangolin"
          text="Your own Pangolin on a VPS."
        />
      </div>

      {mode === "cloud" && <PangolinLimits />}

      <ol className="space-y-5">
        {mode === "cloud" ? (
          <>
            <Step n={1} title="Create an account and an organization">
              Sign up at{" "}
              <a
                href="https://app.pangolin.net"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-foreground underline underline-offset-4"
              >
                app.pangolin.net <ExternalLink className="size-3" />
              </a>{" "}
              and create an organization. Its ID is in the address bar:{" "}
              <code>
                app.pangolin.net/<b>org_xxxx</b>/…
              </code>
              <Field label="Organization ID">
                <Input
                  className="font-mono"
                  placeholder="org_abc123xyz"
                  value={orgId}
                  onChange={(e) => setOrgId(e.target.value)}
                />
              </Field>
            </Step>
            <Step n={2} title="Add a domain for your apps">
              In your organization, open <b>Domains → Add Domain</b>, choose{" "}
              <b>Domain Delegation (NS)</b> and enter a subdomain such as{" "}
              <code>apps.yourdomain.com</code>. At your domain registrar, add
              the NS records Pangolin shows, then wait until the domain is{" "}
              <b>Verified</b>. Every app then gets its own address
              automatically.
            </Step>
          </>
        ) : (
          <>
            <Step n={1} title="Install Pangolin and enable its API">
              Follow the{" "}
              <a
                href="https://docs.pangolin.net/self-host/quick-install"
                target="_blank"
                rel="noreferrer"
                className="text-foreground underline underline-offset-4"
              >
                quick install
              </a>{" "}
              and{" "}
              <a
                href="https://docs.pangolin.net/self-host/advanced/integration-api"
                target="_blank"
                rel="noreferrer"
                className="text-foreground underline underline-offset-4"
              >
                enable the Integration API
              </a>
              . Add a domain for your apps (e.g.{" "}
              <code>apps.yourdomain.com</code> with a wildcard DNS record).
              Details are in <code>docs/pangolin.md</code>.
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="Dashboard URL">
                  <Input
                    className="font-mono"
                    placeholder="https://pangolin.yourdomain.com"
                    value={endpoint}
                    onChange={(e) => setEndpoint(e.target.value)}
                  />
                </Field>
                <Field label="API URL">
                  <Input
                    className="font-mono"
                    placeholder="https://api.yourdomain.com/v1"
                    value={apiUrl}
                    onChange={(e) => setApiUrl(e.target.value)}
                  />
                </Field>
              </div>
              <Field label="Organization ID">
                <Input
                  className="font-mono"
                  value={orgId}
                  onChange={(e) => setOrgId(e.target.value)}
                />
              </Field>
            </Step>
          </>
        )}
        <Step n={mode === "cloud" ? 3 : 2} title="Create an API key">
          Open <b>API Keys → Create API Key</b> in your organization and tick
          these permissions:
          <div className="grid grid-cols-[6rem_1fr] gap-x-4 gap-y-1 rounded-lg border bg-muted/40 p-3 text-xs">
            {permissions.map(([what, perms]) => (
              <div key={what} className="contents">
                <span className="font-medium text-foreground">{what}</span>
                <span>{perms}</span>
              </div>
            ))}
          </div>
          <Field label="API key">
            <Input
              type="password"
              autoComplete="off"
              className="font-mono"
              placeholder={
                settings.pangolin_key_set
                  ? "Saved (leave empty to keep it)"
                  : "Paste the key"
              }
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
            />
          </Field>
        </Step>
        <Step
          n={mode === "cloud" ? 4 : 3}
          title="Connect and choose your apps domain"
        >
          <Button
            type="button"
            variant="outline"
            onClick={test}
            disabled={!canTest || busy !== null}
          >
            {busy === "test" && <Loader2 className="animate-spin" />} Check
            connection
          </Button>
          {domains && domains.length === 0 && (
            <p className="text-sm text-[oklch(0.55_0.13_70)] dark:text-warning">
              Connected, but this organization has no domains yet. Add one in
              Pangolin (step 2), then check again.
            </p>
          )}
          {domains && domains.length > 0 && (
            <div className="space-y-2">
              <p className="flex items-center gap-1.5 text-sm text-success">
                <CheckCircle2 className="size-4" /> Connected. Which domain
                should your apps use?
              </p>
              <div className="grid gap-2">
                {domains.map((d) => (
                  <label
                    key={d.id}
                    className={cn(
                      "flex cursor-pointer items-center justify-between gap-3 rounded-lg border p-3 text-sm",
                      domain === d.domain && "border-primary bg-primary/5",
                    )}
                  >
                    <span className="flex items-center gap-3">
                      <input
                        type="radio"
                        name="apps-domain"
                        checked={domain === d.domain}
                        onChange={() => setDomain(d.domain)}
                      />
                      <span className="font-mono">{d.domain}</span>
                    </span>
                    {d.verified ? (
                      <Badge variant="secondary">verified</Badge>
                    ) : (
                      <Badge variant="outline">not verified yet</Badge>
                    )}
                  </label>
                ))}
              </div>
              {domain && (
                <p className="text-xs text-muted-foreground">
                  Apps will get addresses like{" "}
                  <code>https://my-app-a1b2.{domain}</code>
                </p>
              )}
            </div>
          )}
        </Step>
      </ol>

      {error && (
        <p className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <Button
        onClick={save}
        disabled={!domains?.length || !domain || busy !== null}
      >
        {busy === "save" && <Loader2 className="animate-spin" />} Save and
        connect
      </Button>
    </div>
  );
}

function ChoiceCard({
  active,
  onClick,
  icon: Icon,
  title,
  text,
}: {
  active: boolean;
  onClick: () => void;
  icon: typeof Cloud;
  title: string;
  text: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-start gap-3 rounded-xl border p-4 text-left transition-colors hover:bg-muted/40",
        active && "border-primary bg-primary/5",
      )}
    >
      <Icon className="mt-0.5 size-5 shrink-0 text-primary" />
      <span>
        <span className="block font-medium">{title}</span>
        <span className="block text-sm text-muted-foreground">{text}</span>
      </span>
    </button>
  );
}

function Step({
  n,
  title,
  children,
}: {
  n: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <li className="flex gap-3">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-medium text-primary-foreground">
        {n}
      </span>
      <div className="min-w-0 flex-1 space-y-3 text-sm text-muted-foreground">
        <p className="font-medium text-foreground">{title}</p>
        <div className="space-y-3">{children}</div>
      </div>
    </li>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-foreground">{label}</Label>
      {children}
    </div>
  );
}
