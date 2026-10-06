"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { KeyRound } from "lucide-react";
import { api } from "@/lib/api";
import type { Project } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { PageHeader } from "@/components/page-header";
import { SimpleSelect } from "@/components/simple-select";
import { VariablesTable } from "@/components/variables-table";
import { EmptyState } from "@/components/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function VariablesPage() {
  const load = useCallback(() => api<Project[]>("/projects"), []);
  const { data: projects, loading } = usePoll(load, 30000);
  const [projectId, setProjectId] = useState("");

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- pick the first project once loaded
    if (!projectId && projects?.length) setProjectId(projects[0].id);
  }, [projects, projectId]);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Variables & Secrets"
        description="Environment variables shared by every deployment in a project. Secrets are encrypted at rest and never shown or logged."
      />
      {loading ? (
        <Skeleton className="h-48 w-full rounded-xl" />
      ) : !projects?.length ? (
        <EmptyState
          icon={KeyRound}
          title="No projects yet"
          description="Create a project or deploy something first."
        />
      ) : (
        <Card>
          <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1.5">
              <CardTitle>Project variables</CardTitle>
              <CardDescription>
                Deployment-specific and per-service variables are on each
                deployment&apos;s Environment tab.
              </CardDescription>
              <div className="flex flex-wrap items-center gap-1.5 pt-1 text-xs text-muted-foreground">
                <span>The most specific value wins:</span>
                {["Project", "Environment", "Deployment", "Service"].map(
                  (l, i) => (
                    <span key={l} className="flex items-center gap-1.5">
                      {i > 0 && <span aria-hidden>→</span>}
                      <span className="rounded-md border bg-muted/50 px-1.5 py-0.5 font-medium text-foreground">
                        {l}
                      </span>
                    </span>
                  ),
                )}
              </div>
            </div>
            <SimpleSelect
              className="sm:w-56"
              value={projectId}
              onChange={setProjectId}
              options={projects.map((p) => ({ value: p.id, label: p.name }))}
            />
          </CardHeader>
          <CardContent>
            {projectId && (
              <VariablesTable key={projectId} scope={{ projectId }} />
            )}
          </CardContent>
        </Card>
      )}
      <p className="text-sm text-muted-foreground">
        Private registry credentials are in{" "}
        <Link
          href="/settings"
          className="text-foreground underline underline-offset-4"
        >
          Settings
        </Link>
        .
      </p>
    </div>
  );
}
