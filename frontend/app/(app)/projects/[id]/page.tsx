"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useState } from "react";
import {
  ArrowLeft,
  Boxes,
  FolderKanban,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { Deployment, Domain, Environment, Project } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { deploymentSource, typeLabels } from "@/lib/format";
import { ProjectDialog } from "@/components/project-dialog";
import { DeploymentUrls, StatusBadge, TypeIcon } from "@/components/status";
import { deploymentExposure, ExposureBadge } from "@/components/exposure";
import { cn } from "@/lib/utils";
import { VariablesTable } from "@/components/variables-table";
import { DomainStatus } from "@/components/domain-status";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { Button, buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const envOrder: Environment[] = ["production", "staging", "development"];

export default function ProjectPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const load = useCallback(async () => {
    try {
      return await Promise.all([
        api<Project>(`/projects/${id}`),
        api<Deployment[]>(`/deployments?project_id=${id}`),
        api<Domain[]>("/domains"),
      ]);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404)
        router.replace("/projects");
      throw e;
    }
  }, [id, router]);
  const { data, error, loading, refresh } = usePoll(load, 5000);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (loading)
    return (
      <div className="space-y-6">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-16 w-72" />
        <Skeleton className="h-48 w-full rounded-xl" />
      </div>
    );
  if (!data)
    return error ? <ErrorState message={error} onRetry={refresh} /> : null;
  const [project, deployments, allDomains] = data;
  const domains = allDomains.filter((d) => d.project_id === id);

  async function remove() {
    try {
      await api(`/projects/${id}`, { method: "DELETE" });
      toast.success(`Deleted ${project.name}`);
      router.replace("/projects");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <div className="space-y-6">
      <Link
        href="/projects"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> Projects
      </Link>
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-accent text-accent-foreground">
            <FolderKanban className="size-5" />
          </span>
          <div className="min-w-0 space-y-1.5">
            <h1 className="text-2xl font-semibold tracking-tight">
              {project.name}
            </h1>
            {project.description && (
              <p className="text-sm text-muted-foreground">
                {project.description}
              </p>
            )}
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="rounded-full bg-muted px-2 py-0.5 font-medium tabular-nums">
                {deployments.length} deployment
                {deployments.length === 1 ? "" : "s"}
              </span>
              <span className="rounded-full bg-muted px-2 py-0.5 font-medium tabular-nums">
                {domains.length} domain{domains.length === 1 ? "" : "s"}
              </span>
            </div>
          </div>
        </div>
        <div className="flex gap-2">
          <Link
            href={`/deployments/new?project=${id}`}
            className={buttonVariants()}
          >
            <Plus /> Deploy to {project.name}
          </Link>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="outline"
                  size="icon"
                  aria-label="Project actions"
                />
              }
            >
              <MoreHorizontal />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setEditing(true)}>
                <Pencil /> Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setDeleting(true)}
              >
                <Trash2 /> Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {deployments.length === 0 ? (
        <EmptyState
          icon={Boxes}
          title="No deployments in this project"
          action={
            <Link
              href={`/deployments/new?project=${id}`}
              className={buttonVariants({ size: "sm" })}
            >
              Deploy
            </Link>
          }
        />
      ) : (
        envOrder
          .filter((env) => deployments.some((d) => d.environment === env))
          .map((env) => (
            <Card key={env} className="gap-0 py-0">
              <CardHeader className="border-b py-4">
                <CardTitle className="flex items-center gap-2 text-base capitalize">
                  <span
                    className={cn(
                      "size-2 rounded-full",
                      env === "production"
                        ? "bg-success"
                        : env === "staging"
                          ? "bg-warning"
                          : "bg-info",
                    )}
                  />
                  {env}
                </CardTitle>
              </CardHeader>
              <CardContent className="divide-y p-0">
                {deployments
                  .filter((d) => d.environment === env)
                  .map((d) => (
                    <div
                      key={d.id}
                      className="flex flex-col gap-3 px-4 py-3 transition-colors hover:bg-muted/40 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="flex min-w-0 gap-3">
                        <TypeIcon type={d.type} />
                        <div className="min-w-0">
                          <Link
                            href={`/deployments/${d.id}`}
                            className="font-medium hover:underline"
                          >
                            {d.name}
                          </Link>
                          <p className="truncate font-mono text-xs text-muted-foreground">
                            {typeLabels[d.type]} · {deploymentSource(d)}
                          </p>
                          {d.type === "COMPOSE" && (
                            <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs">
                              {d.services.map((s) => (
                                <span
                                  key={s.id}
                                  className="text-muted-foreground"
                                >
                                  {s.name}{" "}
                                  <span
                                    className={
                                      s.state === "running"
                                        ? "text-success"
                                        : "text-[oklch(0.55_0.13_70)] dark:text-warning"
                                    }
                                  >
                                    {s.state || "—"}
                                  </span>
                                  {!s.public && " · private"}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        <DeploymentUrls d={d} max={3} />
                        <span className="flex flex-wrap items-center justify-end gap-1.5">
                          <ExposureBadge
                            exposure={deploymentExposure(d)}
                            className="hidden sm:inline-flex"
                          />
                          <StatusBadge status={d.status} />
                        </span>
                      </div>
                    </div>
                  ))}
              </CardContent>
            </Card>
          ))
      )}

      <Card>
        <CardHeader>
          <CardTitle>Variables & secrets</CardTitle>
          <CardDescription>
            Shared by every deployment in {project.name}. Limit a variable to
            one environment if it differs between them.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <VariablesTable scope={{ projectId: id }} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div className="space-y-1.5">
            <CardTitle>Domains</CardTitle>
            <CardDescription>
              Custom domains pointing at this project&apos;s services.
            </CardDescription>
          </div>
          <Link
            href="/domains"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Manage
          </Link>
        </CardHeader>
        <CardContent className="space-y-2">
          {domains.length === 0 ? (
            <p className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
              No custom domains yet.
            </p>
          ) : (
            domains.map((dm) => (
              <div
                key={dm.id}
                className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3 text-sm"
              >
                <span className="font-mono font-medium">{dm.hostname}</span>
                <span className="text-muted-foreground">
                  → {dm.target ?? "nothing"}
                </span>
                <DomainStatus domain={dm} />
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <ProjectDialog
        key={project.id + project.name}
        open={editing}
        onOpenChange={setEditing}
        project={project}
        onSaved={() => refresh()}
      />
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${project.name}?`}
        description={
          deployments.length > 0
            ? `Delete its ${deployments.length} deployment(s) first.`
            : "The project's variables and secrets are deleted too."
        }
        confirmLabel="Delete project"
        onConfirm={remove}
      />
    </div>
  );
}
