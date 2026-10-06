"use client";

import { useCallback, useState } from "react";
import { History, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, Revision } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { timeAgo } from "@/lib/format";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState } from "@/components/empty-state";
import { StatusBadge } from "@/components/status";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import type { DeploymentStatus } from "@/lib/types";
import { cn } from "@/lib/utils";

const triggerLabel: Record<Revision["trigger"], string> = {
  deploy: "Deployed",
  redeploy: "Redeployed",
  rollback: "Rollback",
  auto: "New commit",
  config: "Settings changed",
};

export function RevisionsTab({
  d,
  onChanged,
}: {
  d: Deployment;
  onChanged: () => void;
}) {
  const load = useCallback(
    () => api<Revision[]>(`/deployments/${d.id}/revisions`),
    [d.id],
  );
  const { data, loading, refresh } = usePoll(load, 5000);
  const [target, setTarget] = useState<Revision | null>(null);

  async function rollback(r: Revision) {
    try {
      await api(`/deployments/${d.id}/rollback`, {
        method: "POST",
        body: { revision_id: r.id },
      });
      toast.success(`Rolling back to #${r.number}…`);
      refresh();
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  if (loading)
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-20 w-full rounded-xl" />
        ))}
      </div>
    );
  if (!data?.length)
    return <EmptyState icon={History} title="No deployments yet" />;

  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-xs">
      {data.map((r) => {
        const current = r.id === d.revision?.id;
        const what =
          d.type === "IMAGE"
            ? (r.spec.image ?? "") +
              (r.image_digest ? ` @ ${r.image_digest.slice(7, 19)}` : "")
            : r.git_commit
              ? `commit ${r.git_commit.slice(0, 7)}`
              : d.type === "COMPOSE"
                ? `${r.spec.services.length} services`
                : "uploaded project";
        return (
          <div
            key={r.id}
            className={cn(
              "flex flex-wrap items-center justify-between gap-3 border-b p-4 last:border-b-0",
              current &&
                "bg-accent/40 shadow-[inset_3px_0_0_var(--color-primary)]",
            )}
          >
            <div className="flex min-w-0 flex-1 gap-3">
              <span
                className={cn(
                  "flex size-9 shrink-0 items-center justify-center rounded-lg font-mono text-xs font-semibold tabular-nums",
                  current
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted text-muted-foreground",
                )}
              >
                #{r.number}
              </span>
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  {statusBadge(r.status)}
                  {current && <Badge>Current</Badge>}
                  <span className="text-sm text-muted-foreground">
                    {triggerLabel[r.trigger] ?? r.trigger}
                    {r.rollback_of ? ` to #${r.rollback_of}` : ""} ·{" "}
                    {timeAgo(r.created_at)}
                  </span>
                </div>
                <p className="truncate font-mono text-xs text-muted-foreground">
                  {what}
                </p>
                {r.error && (
                  <p className="text-sm whitespace-pre-wrap text-destructive">
                    {r.error}
                  </p>
                )}
              </div>
            </div>
            {!current && r.status !== "FAILED" && (
              <Button variant="outline" size="sm" onClick={() => setTarget(r)}>
                <RotateCcw /> Rollback
              </Button>
            )}
          </div>
        );
      })}
      <ConfirmDialog
        open={!!target}
        onOpenChange={(o) => !o && setTarget(null)}
        title={`Roll back to #${target?.number}?`}
        description={
          d.type === "IMAGE"
            ? "Runs exactly the image that ran in that version (by digest). Variables are the current ones."
            : "Rebuilds that version from the same files or Git commit (or reuses its image if it's still on the machine). Variables are the current ones."
        }
        confirmLabel="Roll back"
        destructive={false}
        onConfirm={() => target && rollback(target)}
      />
    </div>
  );
}

function statusBadge(status: string) {
  if (status === "STOPPED") return <Badge variant="secondary">Replaced</Badge>;
  return <StatusBadge status={status as DeploymentStatus} />;
}
