"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useMemo, useState } from "react";
import { Boxes, Plus, Search, Server, Store } from "lucide-react";
import { api } from "@/lib/api";
import type { Deployment } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { deploymentSource, timeAgo, typeLabels } from "@/lib/format";
import { PageHeader } from "@/components/page-header";
import { DeploymentUrls, StatusBadge, TypeIcon } from "@/components/status";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const filters = {
  all: () => true,
  running: (d: Deployment) => d.status === "RUNNING",
  stopped: (d: Deployment) => d.status === "STOPPED",
  failed: (d: Deployment) => d.status === "FAILED",
} as const;
type Filter = keyof typeof filters;

export default function DeploymentsPage() {
  return (
    <Suspense>
      <DeploymentsList />
    </Suspense>
  );
}

function DeploymentsList() {
  const router = useRouter();
  const params = useSearchParams();
  const filter =
    (params.get("status") as Filter) in filters
      ? (params.get("status") as Filter)
      : "all";
  const [q, setQ] = useState(params.get("q") ?? "");

  const load = useCallback(() => api<Deployment[]>("/deployments"), []);
  const { data, error, loading, refresh } = usePoll(load, 4000);

  const visible = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return (data ?? []).filter(filters[filter]).filter((d) => {
      if (!needle) return true;
      return [
        d.name,
        d.project_name,
        d.agent_name,
        d.status,
        d.type,
        deploymentSource(d),
        d.environment,
      ]
        .join(" ")
        .toLowerCase()
        .includes(needle);
    });
  }, [data, filter, q]);

  const count = (f: Filter) => (data ?? []).filter(filters[f]).length;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Deployments"
        description="Everything running on your machines."
        actions={
          <>
            <Link
              href="/apps"
              className={buttonVariants({ variant: "outline" })}
            >
              <Store /> App Store
            </Link>
            <Link href="/deployments/new" className={buttonVariants()}>
              <Plus /> New deployment
            </Link>
          </>
        }
      />

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <Tabs
          value={filter}
          onValueChange={(v) =>
            router.replace(
              v === "all" ? "/deployments" : `/deployments?status=${v}`,
            )
          }
        >
          <TabsList>
            {(["all", "running", "stopped", "failed"] as const).map((f) => (
              <TabsTrigger
                key={f}
                value={f}
                className="gap-1.5 px-3 capitalize"
              >
                {f}
                <span className="rounded-full bg-muted px-1.5 text-[11px] text-muted-foreground tabular-nums">
                  {count(f)}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <div className="relative sm:w-80">
          <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search by name, image, project or machine"
            className="bg-card pl-8"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            aria-label="Search deployments"
          />
        </div>
      </div>

      {error && <ErrorState message={error} onRetry={refresh} />}
      {loading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-20 w-full rounded-xl" />
          ))}
        </div>
      ) : data?.length === 0 ? (
        <EmptyState
          icon={Boxes}
          title="No deployments yet"
          description="Deploy an app from the App Store, or your own Docker image, Dockerfile or Compose project."
          action={
            <div className="flex gap-2">
              <Link
                href="/apps"
                className={buttonVariants({ variant: "outline", size: "sm" })}
              >
                <Store /> Browse apps
              </Link>
              <Link
                href="/deployments/new"
                className={buttonVariants({ size: "sm" })}
              >
                <Plus /> New deployment
              </Link>
            </div>
          }
        />
      ) : visible.length === 0 ? (
        <div className="rounded-xl border border-dashed py-12 text-center text-sm text-muted-foreground">
          No deployments match{q ? ` "${q}"` : ""}.
        </div>
      ) : (
        <ul className="overflow-hidden rounded-xl border bg-card shadow-xs">
          {visible.map((d) => (
            <li
              key={d.id}
              className="group relative border-b transition-colors last:border-b-0 hover:bg-muted/40"
            >
              <div className="flex flex-col gap-3 p-4 md:flex-row md:items-center md:gap-4">
                <div className="flex min-w-0 flex-1 items-center gap-3">
                  <TypeIcon type={d.type} />
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Link
                        href={`/deployments/${d.id}`}
                        className="font-medium after:absolute after:inset-0 hover:underline"
                      >
                        {d.name}
                      </Link>
                      <Badge variant="secondary" className="font-normal">
                        {d.project_name}
                      </Badge>
                      {d.environment !== "production" && (
                        <Badge
                          variant="outline"
                          className="font-normal capitalize"
                        >
                          {d.environment}
                        </Badge>
                      )}
                    </div>
                    <p className="truncate font-mono text-xs text-muted-foreground">
                      {typeLabels[d.type]}
                      {d.type === "COMPOSE"
                        ? ` · ${d.services.length} services`
                        : ` · ${deploymentSource(d)}`}
                    </p>
                  </div>
                </div>
                <div className="relative z-10 min-w-0 md:w-72">
                  <DeploymentUrls d={d} max={1} />
                </div>
                <div className="flex items-center justify-between gap-4 md:w-64 md:shrink-0 md:justify-end">
                  <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                    <Server className="size-3.5 shrink-0" />
                    <span className="max-w-32 truncate">{d.agent_name}</span>
                    <span>·</span>
                    <span className="whitespace-nowrap">
                      {timeAgo(d.updated_at)}
                    </span>
                  </span>
                  <StatusBadge status={d.status} />
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
