"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { Globe, Plus } from "lucide-react";
import { api } from "@/lib/api";
import type { Deployment, Domain } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { AddDomainDialog } from "@/components/domain-dialog";
import { DomainStatus } from "@/components/domain-status";
import { EmptyState } from "@/components/empty-state";
import { RouteLink } from "@/components/status";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Section } from "@/components/page-header";

export function DomainsTab({ d }: { d: Deployment }) {
  const load = useCallback(() => api<Domain[]>("/domains"), []);
  const { data, refresh } = usePoll(load, 10000);
  const [adding, setAdding] = useState(false);
  const mine = (data ?? []).filter((x) => x.deployment_id === d.id);
  const generated = d.services.flatMap((s) =>
    s.routes.filter((r) => r.kind === "GENERATED").map((r) => ({ s, r })),
  );
  const firstPublic = d.services.find((s) => s.public) ?? d.services[0];

  return (
    <div className="space-y-6">
      <Section
        title="Generated URLs"
        description="Free HTTPS addresses Insta Deploy created for this deployment."
      >
        {generated.length === 0 ? (
          <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">
            No public services. Make a service public on the Services tab.
          </p>
        ) : (
          <div className="space-y-2">
            {generated.map(({ s, r }) => (
              <div key={r.id} className="flex items-center gap-2">
                <RouteLink route={r} />
                {d.services.length > 1 && (
                  <Badge variant="secondary">{s.name}</Badge>
                )}
              </div>
            ))}
          </div>
        )}
      </Section>
      <Section
        title="Custom domains"
        description="Serve this deployment on a domain you own."
        actions={
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Plus /> Add domain
          </Button>
        }
      >
        <div className="space-y-3">
          {mine.length === 0 ? (
            <EmptyState
              icon={Globe}
              title="No custom domains"
              description="Point a domain you own, like app.example.com, at this deployment."
            />
          ) : (
            mine.map((dm) => (
              <div
                key={dm.id}
                className="flex flex-wrap items-center justify-between gap-2 rounded-xl border bg-card p-3 shadow-xs"
              >
                <div className="flex min-w-0 items-center gap-3">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-foreground">
                    <Globe className="size-4" />
                  </span>
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm">{dm.hostname}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      → {dm.target}
                    </p>
                  </div>
                </div>
                <DomainStatus domain={dm} />
              </div>
            ))
          )}
          {mine.length > 0 && (
            <Link
              href="/domains"
              className="text-sm text-muted-foreground underline underline-offset-4"
            >
              Manage domains and DNS records
            </Link>
          )}
        </div>
      </Section>
      <AddDomainDialog
        open={adding}
        onOpenChange={setAdding}
        defaultServiceId={firstPublic?.id}
        onAdded={refresh}
      />
    </div>
  );
}
