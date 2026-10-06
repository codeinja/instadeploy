"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import {
  ArrowLeft,
  ExternalLink,
  Eye,
  EyeOff,
  Globe,
  Info,
  KeyRound,
  Loader2,
  Lock,
  Pencil,
  Rocket,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type {
  Agent,
  CatalogApp,
  ComposeAnalysis,
  Deployment,
  Me,
  Project,
} from "@/lib/types";
import { randomValue, saveDraft } from "@/lib/draft";
import { slug } from "@/lib/format";
import { AppIcon } from "@/components/app-icon";
import { CopyButton } from "@/components/copy-button";
import { SimpleSelect } from "@/components/simple-select";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function AppPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [app, setApp] = useState<CatalogApp | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [me, setMe] = useState<Me | null>(null);
  const [analysis, setAnalysis] = useState<ComposeAnalysis | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const [name, setName] = useState("");
  const [agentId, setAgentId] = useState("");
  const [projectId, setProjectId] = useState("");
  const [deploying, setDeploying] = useState(false);

  useEffect(() => {
    Promise.all([
      api<CatalogApp[]>("/apps"),
      api<Agent[]>("/agents"),
      api<Project[]>("/projects"),
      api<Me>("/me"),
      api<Deployment[]>("/deployments"),
    ])
      .then(async ([apps, a, p, m, deps]) => {
        const found = apps.find((x) => x.id === id);
        if (!found) return router.replace("/apps");
        setApp(found);
        setAgents(a);
        setProjects(p);
        setMe(m);
        setAgentId((a.find((x) => x.status === "ONLINE") ?? a[0])?.id ?? "");
        setProjectId(p.find((x) => x.name === "Default")?.id ?? p[0]?.id ?? "");
        // A free name: "jellyfin", or "jellyfin-2" if taken.
        const taken = new Set(deps.map((d) => d.name));
        let n = slug(found.id);
        for (let i = 2; taken.has(n); i++)
          n = `${slug(found.id).slice(0, 28)}-${i}`;
        setName(n);
        const tz =
          Intl.DateTimeFormat().resolvedOptions().timeZone || "Etc/UTC";
        setValues(
          Object.fromEntries(
            found.inputs.map((i) => [
              i.key,
              i.generate
                ? randomValue(i.generate)
                : i.default === "{tz}"
                  ? tz
                  : (i.default ?? ""),
            ]),
          ),
        );
        setAnalysis(
          await api<ComposeAnalysis>("/analyze/compose", {
            method: "POST",
            body: { content: found.compose },
          }),
        );
      })
      .catch((e) => toast.error(errorMessage(e)));
  }, [id, router]);

  const visibleInputs = useMemo(
    () => app?.inputs.filter((i) => !i.hidden) ?? [],
    [app],
  );

  function variables() {
    return (app?.inputs ?? []).map((i) => ({
      key: i.key,
      value: values[i.key] ?? "",
      secret: i.secret,
    }));
  }

  async function deploy() {
    if (!app || !analysis) return;
    setDeploying(true);
    try {
      const d = await api<Deployment>("/deployments", {
        method: "POST",
        body: {
          agent_id: agentId,
          project_id: projectId,
          name,
          spec: {
            type: "COMPOSE",
            source: { kind: "inline", compose: app.compose },
            services: analysis.services.map((s) =>
              s.name === app.public.service
                ? { name: s.name, public: true, port: app.public.port }
                : { name: s.name, public: false },
            ),
          },
          variables: variables(),
        },
      });
      toast.success(`Deploying ${app.name}…`);
      router.push(`/deployments/${d.id}?tab=logs`);
    } catch (e) {
      toast.error(errorMessage(e));
      setDeploying(false);
    }
  }

  function customize() {
    if (!app) return;
    saveDraft({
      name,
      compose: app.compose,
      env: variables(),
      publicService: app.public.service,
      port: app.public.port,
      source: `${app.name} from the App Store, ready to customize`,
    });
    router.push(
      `/deployments/new?type=COMPOSE&draft=1${projectId ? `&project=${projectId}` : ""}`,
    );
  }

  if (!app)
    return (
      <div className="space-y-6">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-36 w-full rounded-2xl" />
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
          <Skeleton className="h-72 rounded-xl" />
          <Skeleton className="h-72 rounded-xl" />
        </div>
      </div>
    );
  const secretsShown = visibleInputs.some((i) => i.secret);

  return (
    <div className="space-y-6 pb-8">
      <Link
        href="/apps"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> App Store
      </Link>

      <section className="relative overflow-hidden rounded-2xl border bg-card p-6 shadow-xs">
        <div className="pointer-events-none absolute -top-24 -right-24 size-64 rounded-full bg-primary/10 blur-3xl" />
        <div className="relative flex flex-col gap-5 sm:flex-row sm:items-start">
          <span className="flex size-20 shrink-0 items-center justify-center rounded-2xl border bg-background p-3 shadow-sm">
            <AppIcon icon={app.icon} name={app.name} className="size-full" />
          </span>
          <div className="min-w-0 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-2xl font-semibold tracking-tight">
                {app.name}
              </h1>
              <Badge variant="secondary">{app.category}</Badge>
            </div>
            <p className="text-muted-foreground">{app.tagline}</p>
            <p className="max-w-2xl text-sm leading-relaxed">
              {app.description}
            </p>
            <a
              href={app.website}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline"
            >
              Visit website <ExternalLink className="size-3.5" />
            </a>
          </div>
        </div>
      </section>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>What gets deployed</CardTitle>
              <CardDescription>
                {me?.pangolin_enabled
                  ? `${app.public.service} gets a public HTTPS address under ${me.apps_domain}; everything else stays private.`
                  : "Pangolin isn't connected yet, so the app will run without a public URL."}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {!analysis ? (
                <Skeleton className="h-10 w-full" />
              ) : (
                <ul className="divide-y rounded-lg border">
                  {analysis.services.map((s) => (
                    <li
                      key={s.name}
                      className="flex items-center justify-between gap-3 px-3 py-2.5 text-sm"
                    >
                      <span className="min-w-0">
                        <span className="block font-medium">{s.name}</span>
                        <span className="block truncate font-mono text-xs text-muted-foreground">
                          {s.image}
                        </span>
                      </span>
                      {s.name === app.public.service ? (
                        <Badge className="gap-1">
                          <Globe className="size-3" /> Public
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="gap-1">
                          <Lock className="size-3" /> Private
                        </Badge>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          {visibleInputs.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Settings</CardTitle>
                {secretsShown && (
                  <CardDescription className="flex items-start gap-2">
                    <KeyRound className="mt-0.5 size-4 shrink-0" /> Passwords
                    are generated for you and stored encrypted. Copy them now:
                    they can&apos;t be shown again after deploying.
                  </CardDescription>
                )}
              </CardHeader>
              <CardContent className="space-y-4">
                {visibleInputs.map((i) => (
                  <div key={i.key} className="space-y-1.5">
                    <Label htmlFor={i.key}>{i.label}</Label>
                    <div className="flex items-center gap-1">
                      <Input
                        id={i.key}
                        className="font-mono"
                        type={i.secret && !shown[i.key] ? "password" : "text"}
                        autoComplete="off"
                        value={values[i.key] ?? ""}
                        onChange={(e) =>
                          setValues({ ...values, [i.key]: e.target.value })
                        }
                      />
                      {i.secret && (
                        <>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            onClick={() =>
                              setShown({ ...shown, [i.key]: !shown[i.key] })
                            }
                            aria-label={shown[i.key] ? "Hide" : "Show"}
                          >
                            {shown[i.key] ? <EyeOff /> : <Eye />}
                          </Button>
                          <CopyButton value={values[i.key] ?? ""} />
                        </>
                      )}
                    </div>
                    {i.description && (
                      <p className="text-xs text-muted-foreground">
                        {i.description}
                      </p>
                    )}
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          {app.notes && (
            <p className="flex items-start gap-2 rounded-xl border border-info/25 bg-info/5 p-4 text-sm">
              <Info className="mt-0.5 size-4 shrink-0 text-info" /> {app.notes}
            </p>
          )}
        </div>

        <Card className="lg:sticky lg:top-20">
          <CardHeader>
            <CardTitle>Deploy {app.name}</CardTitle>
            <CardDescription>
              Choose where it runs. You can change settings later.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="app-name">Name</Label>
              <Input
                id="app-name"
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
              />
            </div>
            <div className="space-y-1.5">
              <Label>Machine</Label>
              <SimpleSelect
                value={agentId}
                onChange={setAgentId}
                placeholder="Connect a machine first"
                options={agents.map((a) => ({
                  value: a.id,
                  label: `${a.name} (${a.status.toLowerCase()})`,
                  disabled: a.status === "REVOKED",
                }))}
              />
            </div>
            <div className="space-y-1.5">
              <Label>Project</Label>
              <SimpleSelect
                value={projectId}
                onChange={setProjectId}
                placeholder="Default"
                options={projects.map((p) => ({ value: p.id, label: p.name }))}
              />
            </div>
            <div className="space-y-2 pt-2">
              {agents.length === 0 ? (
                <Link
                  href="/setup?step=machine"
                  className={buttonVariants({ className: "w-full" })}
                >
                  Connect a machine first
                </Link>
              ) : (
                <Button
                  size="lg"
                  className="w-full"
                  onClick={deploy}
                  disabled={deploying || !analysis || !agentId || !name}
                >
                  {deploying ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <Rocket />
                  )}{" "}
                  Deploy {app.name}
                </Button>
              )}
              <Button
                variant="ghost"
                className="w-full text-muted-foreground"
                onClick={customize}
              >
                <Pencil /> Customize Compose file
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
