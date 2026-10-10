"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useState } from "react";
import {
  ArrowLeft,
  Boxes,
  CircleAlert,
  ExternalLink,
  FolderKanban,
  Globe,
  History,
  KeyRound,
  Layers,
  LayoutDashboard,
  Loader2,
  MoreHorizontal,
  Play,
  RefreshCw,
  RotateCcw,
  ScrollText,
  Server,
  Square,
  Tag,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { Deployment } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { primaryUrl, typeLabels } from "@/lib/format";
import {
  DeployProgress,
  StatusBadge,
  statusHint,
  TypeIcon,
  UrlChip,
} from "@/components/status";
import { deploymentExposure, ExposureBadge } from "@/components/exposure";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";
import { Button, buttonVariants } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { OverviewTab } from "@/components/deployment/overview-tab";
import { LogsTab } from "@/components/deployment/logs-tab";
import { ServicesTab } from "@/components/deployment/services-tab";
import { RevisionsTab } from "@/components/deployment/revisions-tab";
import { DomainsTab } from "@/components/deployment/domains-tab";
import { VariablesTable } from "@/components/variables-table";

const tabs = [
  "overview",
  "logs",
  "environment",
  "services",
  "domains",
  "deployments",
] as const;

export default function DeploymentPage() {
  return (
    <Suspense>
      <DeploymentDetail />
    </Suspense>
  );
}

function DeploymentDetail() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const params = useSearchParams();
  const tab = tabs.includes(params.get("tab") as (typeof tabs)[number])
    ? params.get("tab")!
    : "overview";

  const load = useCallback(async () => {
    try {
      return await api<Deployment>(`/deployments/${id}`);
    } catch (e) {
      // Deleted (possibly just now, by the agent): back to the list.
      if (e instanceof ApiError && e.status === 404)
        router.replace("/deployments");
      throw e;
    }
  }, [id, router]);
  const { data: d, error, loading, refresh } = usePoll(load, 2500);

  const [confirm, setConfirm] = useState<"stop" | "restart" | "delete" | null>(
    null,
  );
  const [deleteVolumes, setDeleteVolumes] = useState(false);
  const [force, setForce] = useState(false);

  async function act(action: "redeploy" | "restart" | "stop" | "start") {
    try {
      await api(`/deployments/${id}/${action}`, { method: "POST" });
      toast.success(
        {
          redeploy: "Redeploying…",
          restart: "Restarting…",
          stop: "Stopping…",
          start: "Starting…",
        }[action],
      );
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove() {
    try {
      const q = new URLSearchParams();
      if (deleteVolumes) q.set("delete_volumes", "true");
      if (force) q.set("force", "true");
      await api(`/deployments/${id}?${q}`, { method: "DELETE" });
      toast.success(force ? "Deleted" : "Deleting…");
      if (force) router.replace("/deployments");
      else refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  if (loading && !d)
    return (
      <div className="space-y-6">
        <Skeleton className="h-5 w-28" />
        <Skeleton className="h-40 w-full rounded-2xl" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  if (!d)
    return error ? <ErrorState message={error} onRetry={refresh} /> : null;

  const url = primaryUrl(d);
  const hint = statusHint(d);
  const busy = d.status === "DELETING" || !!d.pending_action;

  const inProgress = ["QUEUED", "BUILDING", "DEPLOYING"].includes(d.status);

  return (
    <div className="space-y-6">
      <Link
        href="/deployments"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> Deployments
      </Link>

      <section className="rounded-2xl border bg-card p-5 shadow-xs md:p-6">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex min-w-0 gap-4">
            <TypeIcon
              type={d.type}
              className="size-12 rounded-xl [&_svg]:size-5"
            />
            <div className="min-w-0 space-y-2">
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="truncate text-2xl font-semibold tracking-tight">
                  {d.name}
                </h1>
                <StatusBadge status={d.status} />
                <ExposureBadge exposure={deploymentExposure(d)} />
              </div>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
                <Link
                  href={`/projects/${d.project_id}`}
                  className="inline-flex items-center gap-1.5 hover:text-foreground"
                >
                  <FolderKanban className="size-3.5" /> {d.project_name}
                </Link>
                <span className="inline-flex items-center gap-1.5 capitalize">
                  <Layers className="size-3.5" /> {d.environment}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <Tag className="size-3.5" /> {typeLabels[d.type]}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <Server className="size-3.5" /> {d.agent_name}
                  {d.agent_status !== "ONLINE" && (
                    <span className="font-medium text-[oklch(0.55_0.13_70)] dark:text-warning">
                      (offline)
                    </span>
                  )}
                </span>
              </div>
              {url && <UrlChip url={url} size="lg" className="mt-1" />}
            </div>
          </div>
          <div className="flex flex-wrap gap-2 lg:justify-end">
            {url && (
              <a
                href={url}
                target="_blank"
                rel="noreferrer"
                className={buttonVariants({ variant: "outline" })}
              >
                <ExternalLink /> Open
              </a>
            )}
            {d.status === "STOPPED" ? (
              <Button
                variant="outline"
                onClick={() => act("start")}
                disabled={busy}
              >
                <Play /> Start
              </Button>
            ) : (
              <Button
                variant="outline"
                onClick={() => setConfirm("stop")}
                disabled={busy || d.status === "FAILED"}
              >
                <Square /> Stop
              </Button>
            )}
            <Button
              onClick={() => act("redeploy")}
              disabled={d.status === "DELETING"}
            >
              <RefreshCw /> Redeploy
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="More actions"
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onClick={() => setConfirm("restart")}
                  disabled={busy || d.status !== "RUNNING"}
                >
                  <RotateCcw /> Restart
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setConfirm("delete")}
                >
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {(inProgress || d.status === "FAILED" || hint) && (
          <div className="mt-5 space-y-3 border-t pt-5">
            {(inProgress || d.status === "FAILED") && <DeployProgress d={d} />}
            {hint && (
              <p className="flex items-center gap-2 text-sm text-muted-foreground">
                {(inProgress ||
                  d.status === "DELETING" ||
                  d.pending_action) && (
                  <Loader2 className="size-3.5 animate-spin" />
                )}
                {hint}
              </p>
            )}
          </div>
        )}
      </section>

      {d.status === "FAILED" && d.error && (
        <div className="flex items-start gap-3 rounded-xl border border-destructive/25 bg-destructive/5 p-4 text-sm">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div className="min-w-0 flex-1 space-y-1">
            <p className="font-medium text-destructive">
              This deployment failed
            </p>
            <p className="font-mono text-xs break-words whitespace-pre-wrap text-destructive/90">
              {d.error}
            </p>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => router.replace(`/deployments/${id}?tab=logs`)}
            >
              <ScrollText /> View logs
            </Button>
          </div>
        </div>
      )}

      <Tabs
        value={tab}
        onValueChange={(v) => router.replace(`/deployments/${id}?tab=${v}`)}
      >
        <div className="-mx-4 overflow-x-auto border-b px-4 md:mx-0 md:px-0">
          <TabsList variant="line" className="h-10 gap-4">
            <TabsTrigger value="overview" className="flex-none px-0.5">
              <LayoutDashboard /> Overview
            </TabsTrigger>
            <TabsTrigger value="logs" className="flex-none px-0.5">
              <ScrollText /> Logs
            </TabsTrigger>
            <TabsTrigger value="environment" className="flex-none px-0.5">
              <KeyRound /> Environment
            </TabsTrigger>
            <TabsTrigger value="services" className="flex-none px-0.5">
              <Boxes /> Services
              <span className="rounded-full bg-muted px-1.5 text-[11px] text-muted-foreground tabular-nums">
                {d.services.length}
              </span>
            </TabsTrigger>
            <TabsTrigger value="domains" className="flex-none px-0.5">
              <Globe /> Domains
            </TabsTrigger>
            <TabsTrigger value="deployments" className="flex-none px-0.5">
              <History /> History
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="overview" className="pt-4">
          <OverviewTab d={d} onChanged={refresh} />
        </TabsContent>
        <TabsContent value="logs" className="pt-4">
          <LogsTab d={d} />
        </TabsContent>
        <TabsContent value="environment" className="space-y-3 pt-4">
          <p className="text-sm text-muted-foreground">
            Variables for this deployment. Project-wide variables are on the{" "}
            <Link
              href={`/projects/${d.project_id}`}
              className="font-medium text-foreground underline underline-offset-4"
            >
              {d.project_name}
            </Link>{" "}
            project. Changes apply on the next deploy.
          </p>
          <VariablesTable
            scope={{
              deploymentId: d.id,
              services: d.services.map((s) => s.name),
            }}
            onChanged={() =>
              toast("Redeploy to apply the change", {
                action: { label: "Redeploy", onClick: () => act("redeploy") },
              })
            }
          />
        </TabsContent>
        <TabsContent value="services" className="pt-4">
          <ServicesTab d={d} onChanged={refresh} />
        </TabsContent>
        <TabsContent value="domains" className="pt-4">
          <DomainsTab d={d} />
        </TabsContent>
        <TabsContent value="deployments" className="pt-4">
          <RevisionsTab d={d} onChanged={refresh} />
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        open={confirm === "stop"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Stop ${d.name}?`}
        description="The containers stop and the public URL stops responding until you start it again."
        confirmLabel="Stop"
        onConfirm={() => act("stop")}
      />
      <ConfirmDialog
        open={confirm === "restart"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Restart ${d.name}?`}
        description="The containers restart with their current configuration. There may be a few seconds of downtime."
        confirmLabel="Restart"
        destructive={false}
        onConfirm={() => act("restart")}
      />
      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(o) => {
          if (!o) {
            setConfirm(null);
            setDeleteVolumes(false);
            setForce(false);
          }
        }}
        title={`Delete ${d.name}?`}
        description="This removes the public URL, stops and removes the containers, and deletes the deployment's history."
        confirmLabel="Delete"
        onConfirm={remove}
      >
        <div className="space-y-3 text-sm">
          <label className="flex items-start gap-2">
            <Checkbox
              checked={deleteVolumes}
              onCheckedChange={(v) => setDeleteVolumes(!!v)}
              className="mt-0.5"
            />
            <span>
              Also delete volumes{" "}
              <span className="text-muted-foreground">
                (stored data is lost for good)
              </span>
            </span>
          </label>
          {d.agent_status !== "ONLINE" && (
            <label className="flex items-start gap-2">
              <Checkbox
                checked={force}
                onCheckedChange={(v) => setForce(!!v)}
                className="mt-0.5"
              />
              <span>
                Delete now without waiting for {d.agent_name}{" "}
                <span className="text-muted-foreground">
                  (it&apos;s offline; its containers would be left behind)
                </span>
              </span>
            </label>
          )}
        </div>
      </ConfirmDialog>
    </div>
  );
}
