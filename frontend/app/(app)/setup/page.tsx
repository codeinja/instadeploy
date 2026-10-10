"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import {
  ArrowRight,
  Check,
  CheckCircle2,
  CircleHelp,
  Globe,
  Laptop,
  Loader2,
  Network,
  PartyPopper,
  Rocket,
  Server,
  Store,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Agent, Deployment, ServerSettings } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { cn } from "@/lib/utils";
import { PangolinSetup } from "@/components/pangolin-setup";
import { StatusBadge } from "@/components/status";
import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const steps = [
  { id: "address", title: "Server address", icon: Network, admin: true },
  { id: "pangolin", title: "Public URLs", icon: Globe, admin: true },
  { id: "machine", title: "Connect a machine", icon: Server, admin: false },
  { id: "deploy", title: "First deployment", icon: Rocket, admin: false },
] as const;
type StepId = (typeof steps)[number]["id"];

export default function SetupPage() {
  return (
    <Suspense>
      <Setup />
    </Suspense>
  );
}

// The setup tour: a few guided steps from a fresh install to a running
// app with a public URL. It can be reopened any time from the help button.
function Setup() {
  const router = useRouter();
  const params = useSearchParams();
  const [settings, setSettings] = useState<ServerSettings | null>(null);
  const [step, setStep] = useState<StepId>(
    (params.get("step") as StepId) ?? "address",
  );

  useEffect(() => {
    api<ServerSettings>("/settings")
      .then((s) => {
        setSettings(s);
        // People who can't change server settings start at "machine".
        if (!s.is_admin && !params.get("step")) setStep("machine");
      })
      .catch((e) => toast.error(errorMessage(e)));
  }, [params]);

  const visible = steps.filter((s) => settings?.is_admin || !s.admin);
  const index = visible.findIndex((s) => s.id === step);
  const next = () => {
    const n = visible[index + 1];
    if (n) setStep(n.id);
  };

  async function finish(goTo: string) {
    if (settings?.is_admin)
      await api("/settings", {
        method: "PUT",
        body: { setup_complete: true },
      }).catch(() => {});
    router.push(goTo);
    router.refresh();
  }

  if (!settings)
    return (
      <div className="mx-auto max-w-3xl space-y-6">
        <Skeleton className="mx-auto h-24 w-80" />
        <Skeleton className="h-14 w-full rounded-2xl" />
        <Skeleton className="h-72 w-full rounded-xl" />
      </div>
    );

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div className="space-y-2 text-center">
        <span className="bg-brand mx-auto flex size-12 items-center justify-center rounded-2xl text-white shadow-md shadow-primary/25">
          <Rocket className="size-5" />
        </span>
        <h1 className="pt-2 text-2xl font-semibold tracking-tight">
          Welcome to Insta Deploy
        </h1>
        <p className="mx-auto max-w-xl text-muted-foreground">
          A few steps and you&apos;ll have an app running on your own machine
          with a public HTTPS URL. You can come back here any time from the{" "}
          <CircleHelp className="inline size-4 align-text-bottom" /> button in
          the header.
        </p>
      </div>

      <nav
        aria-label="Setup steps"
        className="rounded-2xl border bg-card p-3 shadow-xs"
      >
        <ol className="flex items-center gap-1">
          {visible.map((s, i) => {
            const done = i < index;
            const current = s.id === step;
            return (
              <li
                key={s.id}
                className={cn(
                  "flex items-center gap-1",
                  i < visible.length - 1 && "flex-1",
                )}
              >
                <button
                  onClick={() => setStep(s.id)}
                  aria-current={current ? "step" : undefined}
                  className={cn(
                    "flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm font-medium whitespace-nowrap transition-colors hover:bg-muted",
                    current ? "text-foreground" : "text-muted-foreground",
                  )}
                >
                  <span
                    className={cn(
                      "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs transition-colors",
                      done && "border-success bg-success text-white",
                      current &&
                        "border-primary bg-primary text-primary-foreground shadow-sm shadow-primary/30",
                      !done && !current && "bg-background",
                    )}
                  >
                    {done ? (
                      <Check className="size-3.5" />
                    ) : (
                      <s.icon className="size-3.5" />
                    )}
                  </span>
                  <span
                    className={cn(
                      "hidden sm:inline",
                      !current && "max-md:hidden",
                    )}
                  >
                    {s.title}
                  </span>
                </button>
                {i < visible.length - 1 && (
                  <span
                    className={cn(
                      "h-px min-w-3 flex-1",
                      done ? "bg-success" : "bg-border",
                    )}
                  />
                )}
              </li>
            );
          })}
        </ol>
      </nav>
      <p className="-mt-3 text-center text-xs text-muted-foreground">
        Step {index + 1} of {visible.length}
      </p>

      {step === "address" && (
        <AddressStep
          settings={settings}
          onSaved={(s) => {
            setSettings(s);
            next();
          }}
        />
      )}

      {step === "pangolin" && (
        <Card>
          <CardHeader>
            <CardTitle>Public URLs with Pangolin</CardTitle>
            <CardDescription>
              Pangolin gives your apps public HTTPS addresses through a secure
              tunnel, so your machines don&apos;t need a public IP or open
              ports. Connect your own Pangolin here.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {settings.pangolin_enabled && (
              <p className="flex flex-wrap items-center gap-2 rounded-lg border border-success/30 bg-success/5 p-3 text-sm">
                <CheckCircle2 className="size-4 text-success" /> Connected. Apps
                get URLs under <code>{settings.apps_domain}</code>.
                <Button size="sm" className="ml-auto" onClick={next}>
                  Continue <ArrowRight />
                </Button>
              </p>
            )}
            <PangolinSetup
              settings={settings}
              onSaved={(s) => {
                setSettings(s);
                next();
              }}
            />
            {!settings.pangolin_enabled && (
              <button
                className="text-sm text-muted-foreground underline underline-offset-4"
                onClick={next}
              >
                Skip for now (apps will run without public URLs)
              </button>
            )}
          </CardContent>
        </Card>
      )}

      {step === "machine" && <MachineStep settings={settings} onDone={next} />}

      {step === "deploy" && <DeployStep onFinish={finish} />}
    </div>
  );
}

function AddressStep({
  settings,
  onSaved,
}: {
  settings: ServerSettings;
  onSaved: (s: ServerSettings) => void;
}) {
  const local = "http://host.docker.internal:8080";
  const guess =
    typeof window !== "undefined" &&
    !["localhost", "127.0.0.1"].includes(window.location.hostname)
      ? `${window.location.protocol}//${window.location.hostname}:8080`
      : "";
  const [mode, setMode] = useState<"local" | "remote">(
    settings.public_api_url === local ? "local" : "remote",
  );
  const [url, setUrl] = useState(
    settings.public_api_url === local ? guess : settings.public_api_url,
  );
  const [saving, setSaving] = useState(false);

  async function save() {
    setSaving(true);
    try {
      const s = await api<ServerSettings>("/settings", {
        method: "PUT",
        body: { public_api_url: mode === "local" ? local : url },
      });
      onSaved(s);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Where will your apps run?</CardTitle>
        <CardDescription>
          Apps run on &quot;machines&quot;: any computer with Docker. Each one
          runs a small agent that connects to this Insta Deploy server.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <label
          className={cn(
            "flex cursor-pointer gap-3 rounded-xl border p-4 transition-colors hover:border-ring/40",
            mode === "local" &&
              "border-primary bg-accent/50 ring-1 ring-primary/30",
          )}
        >
          <input
            type="radio"
            checked={mode === "local"}
            onChange={() => setMode("local")}
            className="mt-1 accent-primary"
          />
          <span>
            <span className="flex items-center gap-2 font-medium">
              <Laptop className="size-4" /> On this same computer
            </span>
            <span className="block text-sm text-muted-foreground">
              Simplest. Insta Deploy and your apps share one machine.
            </span>
          </span>
        </label>
        <label
          className={cn(
            "flex cursor-pointer gap-3 rounded-xl border p-4 transition-colors hover:border-ring/40",
            mode === "remote" &&
              "border-primary bg-accent/50 ring-1 ring-primary/30",
          )}
        >
          <input
            type="radio"
            checked={mode === "remote"}
            onChange={() => setMode("remote")}
            className="mt-1 accent-primary"
          />
          <span className="flex-1 space-y-3">
            <span>
              <span className="flex items-center gap-2 font-medium">
                <Server className="size-4" /> On other machines too
              </span>
              <span className="block text-sm text-muted-foreground">
                Enter the address other machines use to reach this server&apos;s
                port 8080. Use https:// if it&apos;s reachable over the
                internet.
              </span>
            </span>
            {mode === "remote" && (
              <span className="block space-y-1.5">
                <Label htmlFor="api-url">Server address</Label>
                <Input
                  id="api-url"
                  className="font-mono"
                  placeholder="http://192.168.1.10:8080"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                />
              </span>
            )}
          </span>
        </label>
        <Button onClick={save} disabled={saving || (mode === "remote" && !url)}>
          {saving && <Loader2 className="animate-spin" />} Continue{" "}
          <ArrowRight />
        </Button>
      </CardContent>
    </Card>
  );
}

function MachineStep({
  settings,
  onDone,
}: {
  settings: ServerSettings;
  onDone: () => void;
}) {
  const load = useCallback(() => api<Agent[]>("/agents"), []);
  const { data: agents } = usePoll(load, 3000);
  const [name, setName] = useState("My Computer");
  const [created, setCreated] = useState<{
    agent: Agent;
    command: string;
  } | null>(null);
  const [creating, setCreating] = useState(false);
  const live = created && agents?.find((a) => a.id === created.agent.id);
  const online = agents?.filter((a) => a.status === "ONLINE") ?? [];

  async function create() {
    setCreating(true);
    try {
      setCreated(
        await api<{ agent: Agent; command: string }>("/agents", {
          method: "POST",
          body: { name },
        }),
      );
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connect a machine</CardTitle>
        <CardDescription>
          Any computer with Docker: this one, a home server or a VPS. It
          doesn&apos;t need a public IP.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {online.length > 0 && !created && (
          <div className="space-y-2">
            {online.map((a) => (
              <p
                key={a.id}
                className="flex items-center gap-2 rounded-lg border border-success/30 bg-success/5 p-3 text-sm"
              >
                <StatusBadge status="ONLINE" />{" "}
                <span className="font-medium">{a.name}</span>
                <span className="text-muted-foreground">is connected</span>
              </p>
            ))}
          </div>
        )}
        {!created ? (
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex-1 space-y-1.5">
              <Label htmlFor="machine-name">Machine name</Label>
              <Input
                id="machine-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <Button
              onClick={create}
              disabled={creating || !name.trim()}
              variant={online.length ? "outline" : "default"}
            >
              {creating && <Loader2 className="animate-spin" />}{" "}
              {online.length ? "Add another machine" : "Create"}
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            <p className="text-sm">
              Open a terminal <b>on {created.agent.name}</b> and run:
            </p>
            <div className="relative">
              <pre className="overflow-x-auto rounded-xl bg-zinc-950 p-4 pr-12 font-mono text-xs leading-relaxed text-zinc-100">
                {created.command}
              </pre>
              <CopyButton
                value={created.command}
                className="absolute top-2 right-2 text-zinc-300 hover:bg-zinc-800 hover:text-white"
              />
            </div>
            {settings.public_api_url.includes("host.docker.internal") && (
              <p className="text-xs text-muted-foreground">
                This command is for the computer Insta Deploy runs on (see the
                first step).
              </p>
            )}
            <p
              className={cn(
                "flex items-center gap-2 rounded-lg border p-3 text-sm",
                live?.status === "ONLINE" && "border-success/30 bg-success/5",
              )}
            >
              {live?.status === "ONLINE" ? (
                <>
                  <StatusBadge status="ONLINE" /> Connected!
                </>
              ) : (
                <>
                  <Loader2 className="size-4 animate-spin text-muted-foreground" />{" "}
                  Waiting for the machine to connect…
                </>
              )}
            </p>
          </div>
        )}
        <Button onClick={onDone} disabled={online.length === 0}>
          Continue <ArrowRight />
        </Button>
      </CardContent>
    </Card>
  );
}

function DeployStep({ onFinish }: { onFinish: (goTo: string) => void }) {
  const [deploying, setDeploying] = useState(false);
  async function deployDemo() {
    setDeploying(true);
    try {
      const agents = await api<Agent[]>("/agents");
      const agent = agents.find((a) => a.status === "ONLINE");
      if (!agent)
        throw new Error(
          "No machine is online. Go back one step and connect one.",
        );
      const existing = (await api<Deployment[]>("/deployments")).map(
        (d) => d.name,
      );
      let name = "hello";
      for (let i = 2; existing.includes(name); i++) name = `hello-${i}`;
      const d = await api<Deployment>("/deployments", {
        method: "POST",
        body: {
          agent_id: agent.id,
          name,
          spec: {
            type: "IMAGE",
            image: "nginx:latest",
            services: [{ name: "web", port: 80, public: true }],
          },
        },
      });
      onFinish(`/deployments/${d.id}?tab=logs`);
    } catch (e) {
      toast.error(errorMessage(e));
      setDeploying(false);
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PartyPopper className="size-5" /> Deploy your first app
        </CardTitle>
        <CardDescription>
          Try it with nginx&apos;s welcome page: it gets a public URL right away
          and holds no data. Everything else you deploy starts private until you
          choose to make it public.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap gap-2">
        <Button size="lg" onClick={deployDemo} disabled={deploying}>
          {deploying ? <Loader2 className="animate-spin" /> : <Rocket />} Deploy
          a public test page
        </Button>
        <Button
          size="lg"
          variant="outline"
          onClick={() => onFinish("/deployments/new")}
        >
          Deploy my own
        </Button>
        <Button size="lg" variant="outline" onClick={() => onFinish("/apps")}>
          <Store /> Browse the App Store
        </Button>
        <Button size="lg" variant="ghost" onClick={() => onFinish("/")}>
          Finish
        </Button>
      </CardContent>
    </Card>
  );
}
