"use client";

import Link from "next/link";
import { useCallback } from "react";
import {
  ArrowRight,
  Boxes,
  CheckCircle2,
  CircleAlert,
  Globe,
  Plus,
  Rocket,
  Server,
  Sparkles,
  Store,
} from "lucide-react";
import { api } from "@/lib/api";
import type { ActivityEvent, Agent, Deployment, Me } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { deploymentSource, greeting, timeAgo } from "@/lib/format";
import { DeploymentUrls, StatusBadge, TypeIcon } from "@/components/status";
import { deploymentExposure, ExposureBadge } from "@/components/exposure";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { ActivityItem } from "@/components/activity-item";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export default function OverviewPage() {
  const load = useCallback(
    () =>
      Promise.all([
        api<Deployment[]>("/deployments"),
        api<Agent[]>("/agents"),
        api<ActivityEvent[]>("/activity?limit=8"),
        api<Me>("/me"),
      ]),
    [],
  );
  const { data, error, loading, refresh } = usePoll(load, 5000);
  const [deployments, agents, activity, me] = data ?? [];

  const running =
    deployments?.filter((d) => d.status === "RUNNING").length ?? 0;
  const failed = deployments?.filter((d) => d.status === "FAILED") ?? [];
  const publicUrls =
    deployments?.reduce(
      (n, d) =>
        n +
        d.services.reduce(
          (m, s) => m + s.routes.filter((r) => r.status === "READY").length,
          0,
        ),
      0,
    ) ?? 0;
  const online = agents?.filter((a) => a.status === "ONLINE").length ?? 0;
  const needsSetup =
    me && (!me.setup_complete || !me.pangolin_enabled || agents?.length === 0);

  return (
    <div className="space-y-8">
      {/* Hero */}
      <section className="relative overflow-hidden rounded-2xl border bg-card p-6 shadow-xs md:p-8">
        <div className="pointer-events-none absolute -top-24 -right-24 size-72 rounded-full bg-primary/15 blur-3xl" />
        <div className="pointer-events-none absolute -bottom-32 left-1/3 size-72 rounded-full bg-info/10 blur-3xl" />
        <div className="relative flex flex-col gap-6 md:flex-row md:items-end md:justify-between">
          <div className="space-y-2">
            <p className="text-sm font-medium text-primary">{greeting()}</p>
            <h1 className="text-3xl font-semibold tracking-tight">
              {me ? `Welcome back, ${me.name.split(" ")[0]}` : "Welcome back"}
            </h1>
            <p className="max-w-lg text-muted-foreground">
              {deployments?.length
                ? `${running} of ${deployments.length} deployment${deployments.length === 1 ? " is" : "s are"} running on ${online} machine${online === 1 ? "" : "s"}.`
                : "Run anything Docker can run, on your own machines, with a public HTTPS URL."}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Link
              href="/deployments/new"
              className={buttonVariants({ size: "lg" })}
            >
              <Plus /> New deployment
            </Link>
            <Link
              href="/apps"
              className={buttonVariants({ size: "lg", variant: "outline" })}
            >
              <Store /> App Store
            </Link>
          </div>
        </div>
      </section>

      {error && <ErrorState message={error} onRetry={refresh} />}

      {needsSetup && (
        <Link href="/setup" className="group block">
          <div className="flex items-center gap-4 rounded-xl border border-primary/30 bg-accent/60 p-4 transition-colors group-hover:bg-accent">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <Sparkles className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="font-medium">Finish setting up</p>
              <p className="text-sm text-muted-foreground">
                {!me.pangolin_enabled && me.is_admin
                  ? "Connect Pangolin to give your apps public URLs, and connect a machine to run them."
                  : !me.pangolin_enabled
                    ? "Public URLs aren't set up yet: ask the server's admin to connect Pangolin."
                    : agents?.length === 0
                      ? "Connect a machine to run your apps on."
                      : "A few short steps to your first app with a public URL."}
              </p>
            </div>
            <ArrowRight className="size-5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
          </div>
        </Link>
      )}

      {/* Stats */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat
          label="Deployments"
          value={deployments?.length}
          icon={Boxes}
          tone="primary"
          href="/deployments"
          loading={loading}
        />
        <Stat
          label="Running"
          value={running}
          icon={CheckCircle2}
          tone="success"
          href="/deployments?status=running"
          loading={loading}
        />
        <Stat
          label="Machines online"
          value={agents ? `${online}/${agents.length}` : undefined}
          icon={Server}
          tone="info"
          href="/agents"
          loading={loading}
        />
        <Stat
          label="Public URLs"
          value={publicUrls}
          icon={Globe}
          tone="warning"
          href="/domains"
          loading={loading}
        />
      </div>

      {failed.length > 0 && (
        <div className="flex items-start gap-3 rounded-xl border border-destructive/25 bg-destructive/5 p-4 text-sm">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div className="space-y-1">
            <p className="font-medium text-destructive">
              {failed.length} deployment{failed.length > 1 ? "s" : ""} failed
            </p>
            {failed.slice(0, 3).map((d) => (
              <p key={d.id} className="text-muted-foreground">
                <Link
                  href={`/deployments/${d.id}`}
                  className="font-medium text-foreground hover:underline"
                >
                  {d.name}
                </Link>
                : {d.error.split("\n")[0]}
              </p>
            ))}
          </div>
        </div>
      )}

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <Card className="gap-0 py-0">
          <CardHeader className="flex flex-row items-center justify-between border-b py-4">
            <CardTitle className="text-base">Recent deployments</CardTitle>
            <Link
              href="/deployments"
              className="text-sm text-muted-foreground hover:text-foreground"
            >
              View all
            </Link>
          </CardHeader>
          <CardContent className="p-0">
            {loading && (
              <div className="space-y-3 p-4">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            )}
            {deployments?.length === 0 && (
              <div className="p-4">
                <EmptyState
                  icon={Rocket}
                  title="Nothing deployed yet"
                  description="Pick an app from the App Store, or deploy your own image, Dockerfile or Compose project."
                  action={
                    <Link
                      href={agents?.length ? "/deployments/new" : "/setup"}
                      className={buttonVariants({ size: "sm" })}
                    >
                      {agents?.length
                        ? "Deploy something"
                        : "Connect a machine first"}
                    </Link>
                  }
                  className="border-none bg-transparent"
                />
              </div>
            )}
            <ul className="divide-y">
              {deployments?.slice(0, 6).map((d) => (
                <li
                  key={d.id}
                  className="group relative flex items-center gap-3 px-4 py-3 transition-colors hover:bg-muted/50"
                >
                  <TypeIcon type={d.type} />
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <Link
                        href={`/deployments/${d.id}`}
                        className="truncate font-medium after:absolute after:inset-0"
                      >
                        {d.name}
                      </Link>
                      <span className="hidden truncate text-xs text-muted-foreground sm:inline">
                        {deploymentSource(d)}
                      </span>
                    </div>
                    <div className="relative z-10 w-fit max-w-full">
                      <DeploymentUrls d={d} max={1} />
                    </div>
                  </div>
                  <span className="flex flex-wrap items-center justify-end gap-1.5">
                    <ExposureBadge
                      exposure={deploymentExposure(d)}
                      className="hidden sm:inline-flex"
                    />
                    <StatusBadge status={d.status} />
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>

        <div className="space-y-6">
          <Card className="gap-0 py-0">
            <CardHeader className="flex flex-row items-center justify-between border-b py-4">
              <CardTitle className="text-base">Machines</CardTitle>
              <Link
                href="/agents"
                className="text-sm text-muted-foreground hover:text-foreground"
              >
                Manage
              </Link>
            </CardHeader>
            <CardContent className="space-y-1 p-2">
              {loading && <Skeleton className="m-2 h-10" />}
              {agents?.length === 0 && (
                <p className="p-2 text-sm text-muted-foreground">
                  No machines yet.{" "}
                  <Link
                    href="/setup?step=machine"
                    className="font-medium text-primary hover:underline"
                  >
                    Connect one
                  </Link>
                </p>
              )}
              {agents?.map((a) => (
                <Link
                  key={a.id}
                  href="/agents"
                  className="flex items-center gap-3 rounded-lg p-2 transition-colors hover:bg-muted/60"
                >
                  <span className="flex size-8 items-center justify-center rounded-md bg-muted">
                    <Server className="size-4 text-muted-foreground" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{a.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {a.deployments} app{a.deployments === 1 ? "" : "s"} · seen{" "}
                      {timeAgo(a.last_seen)}
                    </p>
                  </div>
                  <StatusBadge status={a.status} />
                </Link>
              ))}
            </CardContent>
          </Card>

          <Card className="gap-0 py-0">
            <CardHeader className="flex flex-row items-center justify-between border-b py-4">
              <CardTitle className="text-base">Activity</CardTitle>
              <Link
                href="/activity"
                className="text-sm text-muted-foreground hover:text-foreground"
              >
                View all
              </Link>
            </CardHeader>
            <CardContent className="p-4">
              {activity?.length === 0 && (
                <p className="text-sm text-muted-foreground">Nothing yet.</p>
              )}
              <ol className="space-y-0">
                {activity?.slice(0, 6).map((e, i, all) => (
                  <ActivityItem
                    key={e.id}
                    event={e}
                    last={i === all.length - 1}
                    compact
                  />
                ))}
              </ol>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

const tones = {
  primary: "bg-primary/10 text-primary",
  success: "bg-success/12 text-success",
  info: "bg-info/12 text-info",
  warning: "bg-warning/15 text-[oklch(0.55_0.13_70)] dark:text-warning",
};

function Stat({
  label,
  value,
  icon: Icon,
  tone,
  href,
  loading,
}: {
  label: string;
  value: number | string | undefined;
  icon: typeof Boxes;
  tone: keyof typeof tones;
  href: string;
  loading: boolean;
}) {
  return (
    <Link href={href} className="group">
      <Card className="h-full py-4 transition-all group-hover:-translate-y-0.5 group-hover:shadow-md">
        <CardContent className="flex items-center gap-3 px-4">
          <span
            className={cn(
              "flex size-10 shrink-0 items-center justify-center rounded-lg",
              tones[tone],
            )}
          >
            <Icon className="size-5" />
          </span>
          <div className="min-w-0">
            <p className="truncate text-xs font-medium text-muted-foreground">
              {label}
            </p>
            {loading ? (
              <Skeleton className="mt-1 h-6 w-10" />
            ) : (
              <p className="text-2xl leading-tight font-semibold tabular-nums">
                {value ?? 0}
              </p>
            )}
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
