"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { ArrowRight, FolderKanban, Plus } from "lucide-react";
import { api } from "@/lib/api";
import type { Project } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { PageHeader } from "@/components/page-header";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { ProjectDialog } from "@/components/project-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function ProjectsPage() {
  const load = useCallback(() => api<Project[]>("/projects"), []);
  const { data, error, loading, refresh } = usePoll(load, 10000);
  const [creating, setCreating] = useState(false);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Projects"
        description="Group related deployments, domains, variables and secrets."
        actions={
          <Button onClick={() => setCreating(true)}>
            <Plus /> New project
          </Button>
        }
      />
      {error && <ErrorState message={error} onRetry={refresh} />}
      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : data?.length === 0 ? (
        <EmptyState
          icon={FolderKanban}
          title="No projects yet"
          description="Projects are created automatically when you deploy, or you can create one now."
          action={
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus /> New project
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data?.map((p) => (
            <Link
              key={p.id}
              href={`/projects/${p.id}`}
              className="group rounded-xl focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <Card className="h-full transition-all group-hover:-translate-y-0.5 group-hover:border-primary/40 group-hover:shadow-md">
                <CardHeader>
                  <div className="mb-1 flex items-start justify-between">
                    <span className="flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground">
                      <FolderKanban className="size-5" />
                    </span>
                    <ArrowRight className="size-4 text-muted-foreground opacity-0 transition-all group-hover:translate-x-0.5 group-hover:opacity-100" />
                  </div>
                  <CardTitle>{p.name}</CardTitle>
                  <CardDescription className="line-clamp-2">
                    {p.description || "No description"}
                  </CardDescription>
                </CardHeader>
                <CardContent className="mt-auto flex flex-wrap items-center gap-2 text-xs">
                  <span className="rounded-full bg-muted px-2 py-0.5 font-medium tabular-nums">
                    {p.deployments} deployment{p.deployments === 1 ? "" : "s"}
                  </span>
                  {p.running > 0 && (
                    <span className="rounded-full bg-success/12 px-2 py-0.5 font-medium text-success tabular-nums">
                      {p.running} running
                    </span>
                  )}
                  {p.failed > 0 && (
                    <span className="rounded-full bg-destructive/10 px-2 py-0.5 font-medium text-destructive tabular-nums">
                      {p.failed} failed
                    </span>
                  )}
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
      <ProjectDialog
        open={creating}
        onOpenChange={setCreating}
        onSaved={refresh}
      />
    </div>
  );
}
