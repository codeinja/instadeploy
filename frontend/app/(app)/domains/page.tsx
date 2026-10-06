"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import {
  ChevronDown,
  Globe,
  Loader2,
  Plus,
  RefreshCw,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, Domain, Me } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { PageHeader } from "@/components/page-header";
import { AddDomainDialog } from "@/components/domain-dialog";
import { DnsRecords } from "@/components/dns-records";
import { DomainStatus } from "@/components/domain-status";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState } from "@/components/empty-state";
import { PangolinLimits } from "@/components/pangolin-limits";
import { ErrorState } from "@/components/error-state";
import { SimpleSelect } from "@/components/simple-select";
import { RouteLink } from "@/components/status";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { cn } from "@/lib/utils";

export default function DomainsPage() {
  const load = useCallback(
    () =>
      Promise.all([
        api<Domain[]>("/domains"),
        api<Deployment[]>("/deployments"),
        api<Me>("/me"),
      ]),
    [],
  );
  const { data, error, loading, refresh } = usePoll(load, 8000);
  const [adding, setAdding] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Domain | null>(null);
  const [checking, setChecking] = useState<string | null>(null);
  const [domains, deployments, me] = data ?? [];

  const serviceOptions = (deployments ?? []).flatMap((d) =>
    d.services
      .filter((s) => s.port)
      .map((s) => ({
        value: s.id,
        label: d.type === "COMPOSE" ? `${d.name} / ${s.name}` : d.name,
      })),
  );
  const generated = (deployments ?? []).flatMap((d) =>
    d.services.flatMap((s) =>
      s.routes.filter((r) => r.kind === "GENERATED").map((r) => ({ d, s, r })),
    ),
  );

  async function retarget(dm: Domain, serviceId: string) {
    try {
      await api(`/domains/${dm.id}`, {
        method: "PATCH",
        body: { service_id: serviceId || null },
      });
      toast.success(serviceId ? "Domain re-pointed" : "Domain detached");
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function checkAgain(dm: Domain) {
    setChecking(dm.id);
    try {
      await api(`/domains/${dm.id}/verify`, { method: "POST" });
      toast.success("Checking again", {
        description:
          "Verification usually takes under a minute once the records are visible.",
      });
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setChecking(null);
    }
  }

  async function remove(dm: Domain) {
    try {
      await api(`/domains/${dm.id}`, { method: "DELETE" });
      toast.success(`Removed ${dm.hostname}`);
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Domains"
        description="Use your own domains, like app.example.com, for your deployments."
        actions={
          <Button
            onClick={() => setAdding(true)}
            disabled={me && !me.pangolin_enabled}
          >
            <Plus /> Add domain
          </Button>
        }
      />
      {error && <ErrorState message={error} onRetry={refresh} />}
      {me && !me.pangolin_enabled && (
        <p className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/10 p-4 text-sm">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-[oklch(0.55_0.13_70)] dark:text-warning" />
          <span>
            Custom domains need Pangolin.{" "}
            {me.is_admin ? (
              <Link
                href="/setup?step=pangolin"
                className="font-medium underline underline-offset-4"
              >
                Connect it in the setup guide
              </Link>
            ) : (
              "Ask the server's admin to connect it."
            )}
          </span>
        </p>
      )}

      {me?.pangolin_enabled && domains && (
        <PangolinLimits
          usage={{
            urls:
              generated.length +
              domains.filter((d) => d.status === "ACTIVE").length,
            domains: domains.length + 1,
          }}
        />
      )}

      {loading ? (
        <Skeleton className="h-32 w-full rounded-xl" />
      ) : domains?.length === 0 ? (
        <EmptyState
          icon={Globe}
          title="No custom domains"
          description="Add a domain, create the DNS records Insta Deploy shows you, and it will be connected with HTTPS automatically."
          action={
            <Button
              size="sm"
              onClick={() => setAdding(true)}
              disabled={me && !me.pangolin_enabled}
            >
              <Plus /> Add domain
            </Button>
          }
        />
      ) : (
        <Card className="gap-0 py-0">
          <CardContent className="divide-y p-0">
            <div className="hidden grid-cols-[2fr_2fr_2fr_auto] gap-4 bg-muted/40 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid">
              <span>Domain</span>
              <span>Status</span>
              <span>Target</span>
              <span className="w-16" />
            </div>
            {domains?.map((dm) => (
              <div key={dm.id}>
                <div className="grid items-center gap-2 px-4 py-3 transition-colors hover:bg-muted/30 md:grid-cols-[2fr_2fr_2fr_auto] md:gap-4">
                  <button
                    className="flex min-w-0 items-center gap-2 text-left font-mono text-sm font-medium"
                    aria-expanded={expanded === dm.id}
                    onClick={() =>
                      setExpanded(expanded === dm.id ? null : dm.id)
                    }
                  >
                    <ChevronDown
                      className={cn(
                        "size-4 shrink-0 text-muted-foreground transition-transform",
                        expanded === dm.id && "rotate-180",
                      )}
                    />
                    <span className="truncate">{dm.hostname}</span>
                  </button>
                  <DomainStatus domain={dm} />
                  <SimpleSelect
                    value={dm.service_id ?? ""}
                    onChange={(v) => retarget(dm, v)}
                    options={[
                      { value: "", label: "Not pointing anywhere" },
                      ...serviceOptions,
                    ]}
                  />
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setDeleting(dm)}
                    aria-label={`Remove ${dm.hostname}`}
                  >
                    <Trash2 />
                  </Button>
                </div>
                {expanded === dm.id && (
                  <div className="space-y-3 border-t border-dashed bg-muted/30 px-4 py-4">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <p className="text-sm text-muted-foreground">
                        {dm.status === "ACTIVE"
                          ? "DNS is verified. Keep these records in place."
                          : "Create these records where your domain's DNS is managed. Insta Deploy checks automatically; new records can take a few minutes to appear."}
                      </p>
                      {dm.status === "PENDING" && (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => checkAgain(dm)}
                          disabled={checking === dm.id}
                        >
                          {checking === dm.id ? (
                            <Loader2 className="animate-spin" />
                          ) : (
                            <RefreshCw />
                          )}{" "}
                          Check again
                        </Button>
                      )}
                    </div>
                    <DnsRecords records={dm.dns_records} />
                  </div>
                )}
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Generated URLs</CardTitle>
          <CardDescription>
            Every public service automatically gets an address under{" "}
            {me ? <code>{me.apps_domain}</code> : "your apps domain"}.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {generated.length === 0 && (
            <p className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
              No public services yet.
            </p>
          )}
          {generated.map(({ d, s, r }) => (
            <div
              key={r.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-2 pl-2"
            >
              <RouteLink route={r} />
              <Link
                href={`/deployments/${d.id}`}
                className="pr-2 text-sm text-muted-foreground hover:text-foreground hover:underline"
              >
                {d.type === "COMPOSE" ? `${d.name} / ${s.name}` : d.name}
              </Link>
            </div>
          ))}
        </CardContent>
      </Card>

      <AddDomainDialog
        open={adding}
        onOpenChange={setAdding}
        onAdded={refresh}
      />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Remove ${deleting?.hostname}?`}
        description="The domain stops serving your deployment and is removed from Pangolin. You can delete its DNS records afterwards."
        confirmLabel="Remove"
        onConfirm={() => deleting && remove(deleting)}
      />
    </div>
  );
}
