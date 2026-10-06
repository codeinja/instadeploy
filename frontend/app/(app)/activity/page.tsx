"use client";

import { useCallback, useMemo } from "react";
import { Activity } from "lucide-react";
import { api } from "@/lib/api";
import type { ActivityEvent } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { PageHeader } from "@/components/page-header";
import { ActivityItem } from "@/components/activity-item";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";

// "Today", "Yesterday", or a date.
function dayLabel(iso: string): string {
  const d = new Date(iso);
  const today = new Date();
  const yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return d.toLocaleDateString(undefined, {
    weekday: "long",
    month: "short",
    day: "numeric",
  });
}

export default function ActivityPage() {
  const load = useCallback(
    () => api<ActivityEvent[]>("/activity?limit=200"),
    [],
  );
  const { data, error, loading, refresh } = usePoll(load, 5000);

  const days = useMemo(() => {
    const groups: { label: string; events: ActivityEvent[] }[] = [];
    for (const e of data ?? []) {
      const label = dayLabel(e.created_at);
      if (groups.at(-1)?.label === label) groups.at(-1)!.events.push(e);
      else groups.push({ label, events: [e] });
    }
    return groups;
  }, [data]);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Activity"
        description="What happened recently: deploys, failures, machines connecting, domains and settings."
      />
      {error && <ErrorState message={error} onRetry={refresh} />}
      {loading ? (
        <Skeleton className="h-64 w-full rounded-xl" />
      ) : data?.length === 0 ? (
        <EmptyState
          icon={Activity}
          title="No activity yet"
          description="Deploys, failures and changes will show up here."
        />
      ) : (
        <div className="space-y-6">
          {days.map((g) => (
            <section key={g.label} className="space-y-2">
              <h2 className="sticky top-14 z-10 w-fit rounded-full border bg-background/90 px-3 py-0.5 text-xs font-medium text-muted-foreground backdrop-blur">
                {g.label}
              </h2>
              <Card>
                <CardContent>
                  <ol>
                    {g.events.map((e, i) => (
                      <ActivityItem
                        key={e.id}
                        event={e}
                        last={i === g.events.length - 1}
                      />
                    ))}
                  </ol>
                </CardContent>
              </Card>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
