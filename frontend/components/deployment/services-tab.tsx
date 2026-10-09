"use client";

import { useState } from "react";
import { HeartPulse, Loader2, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, Service } from "@/lib/types";
import { formatBytes } from "@/lib/format";
import { ContainerState, HealthBadge, RouteLink } from "@/components/status";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

// How the project's other deployments (same environment and agent) reach a service
// on the project network: mirrors privateHost in backend/spec.go.
function privateAddress(d: Deployment, s: Service) {
  const host =
    d.type === "COMPOSE" ? `${s.name.toLowerCase()}.${d.name}` : d.name;
  return s.port ? `${host}:${s.port}` : host;
}

export function ServicesTab({
  d,
  onChanged,
}: {
  d: Deployment;
  onChanged: () => void;
}) {
  const [healthFor, setHealthFor] = useState<Service | null>(null);

  async function update(
    s: Service,
    body: Record<string, unknown>,
    message: string,
  ) {
    try {
      await api(`/deployments/${d.id}/services/${encodeURIComponent(s.name)}`, {
        method: "PATCH",
        body,
      });
      toast.success(
        message,
        d.type === "COMPOSE" && "public" in body
          ? {
              description:
                "Redeploying so the service can join or leave the tunnel.",
            }
          : undefined,
      );
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function retryRoutes() {
    try {
      await api(`/deployments/${d.id}/routes/retry`, { method: "POST" });
      toast.success("Retrying…");
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const failedRoutes = d.services.flatMap((s) =>
    s.routes.filter((r) => r.status === "FAILED"),
  );

  return (
    <div className="space-y-4">
      {failedRoutes.length > 0 && (
        <div className="flex items-start justify-between gap-4 rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm">
          <div className="space-y-1">
            <p className="font-medium text-destructive">Public URL FAILED</p>
            {failedRoutes.map((r) => (
              <p key={r.id} className="text-destructive">
                {r.hostname}: {r.error}
              </p>
            ))}
          </div>
          <Button size="sm" variant="outline" onClick={retryRoutes}>
            <RotateCw /> Retry
          </Button>
        </div>
      )}
      <div className="overflow-x-auto rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Service</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-24">Public</TableHead>
              <TableHead className="w-32">Port</TableHead>
              <TableHead>URL</TableHead>
              <TableHead className="hidden xl:table-cell">Usage</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.services.map((s) => (
              <TableRow key={s.id}>
                <TableCell>
                  <div className="font-medium">{s.name}</div>
                  <div
                    className="max-w-56 truncate font-mono text-xs text-muted-foreground"
                    title={s.image}
                  >
                    {s.image}
                  </div>
                  <div
                    className="max-w-56 truncate font-mono text-xs text-muted-foreground"
                    title={`Private address for this project's other ${d.environment} deployments`}
                  >
                    {privateAddress(d, s)}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-col items-start gap-1">
                    <ContainerState state={s.state} />
                    <button
                      onClick={() => setHealthFor(s)}
                      title="Health check settings"
                    >
                      <HealthBadge health={s.health} />
                    </button>
                  </div>
                </TableCell>
                <TableCell>
                  <Switch
                    checked={s.public}
                    disabled={d.status === "DELETING"}
                    onCheckedChange={(v) => {
                      if (v && !s.port)
                        return toast.error(`Set a port for ${s.name} first`);
                      update(
                        s,
                        { public: v },
                        v
                          ? `${s.name} is now public`
                          : `${s.name} is now private`,
                      );
                    }}
                    aria-label={`Make ${s.name} public`}
                  />
                </TableCell>
                <TableCell>
                  <PortInput
                    s={s}
                    onSave={(port) =>
                      update(s, { port }, `Port of ${s.name} set to ${port}`)
                    }
                  />
                </TableCell>
                <TableCell className="max-w-80">
                  {s.routes.length === 0 ? (
                    <span className="text-sm text-muted-foreground">
                      {s.public ? "—" : "Private"}
                    </span>
                  ) : (
                    <div className="flex flex-col gap-1">
                      {s.routes.map((r) => (
                        <span key={r.id} className="flex items-center gap-2">
                          <RouteLink route={r} />
                          {r.kind === "CUSTOM" && (
                            <Badge variant="secondary">custom</Badge>
                          )}
                        </span>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell className="hidden text-xs text-muted-foreground xl:table-cell">
                  {s.stats
                    ? `${s.stats.cpu_percent.toFixed(1)}% CPU · ${formatBytes(s.stats.memory_bytes)}`
                    : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {d.type === "COMPOSE" && (
        <p className="text-xs text-muted-foreground">
          Making a Compose service public or private redeploys the project, so
          the service can join or leave the tunnel network. Private services are
          only reachable by the other services in the project.
        </p>
      )}
      {healthFor && (
        <HealthDialog
          d={d}
          s={healthFor}
          onClose={() => setHealthFor(null)}
          onSaved={onChanged}
        />
      )}
    </div>
  );
}

function PortInput({
  s,
  onSave,
}: {
  s: Service;
  onSave: (port: number) => void;
}) {
  const [value, setValue] = useState(s.port ? String(s.port) : "");
  const dirty = value !== (s.port ? String(s.port) : "");
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (value) onSave(Number(value));
      }}
      className="flex items-center gap-1"
    >
      <Input
        type="number"
        min={1}
        max={65535}
        className="h-7 w-24"
        placeholder={s.detected_ports.join(", ") || "—"}
        value={value}
        list={`svc-ports-${s.id}`}
        onChange={(e) => setValue(e.target.value)}
        aria-label={`Port of ${s.name}`}
      />
      <datalist id={`svc-ports-${s.id}`}>
        {s.detected_ports.map((p) => (
          <option key={p} value={p} />
        ))}
      </datalist>
      {dirty && value && (
        <Button type="submit" size="xs" variant="outline">
          Save
        </Button>
      )}
    </form>
  );
}

function HealthDialog({
  d,
  s,
  onClose,
  onSaved,
}: {
  d: Deployment;
  s: Service;
  onClose: () => void;
  onSaved: () => void;
}) {
  const current = d.spec.services.find((x) => x.name === s.name)?.health_check;
  const [path, setPath] = useState(current?.path ?? "/health");
  const [interval, setInterval] = useState(
    String(current?.interval_seconds ?? 30),
  );
  const [saving, setSaving] = useState(false);

  async function save(body: Record<string, unknown>) {
    setSaving(true);
    try {
      await api(`/deployments/${d.id}/services/${encodeURIComponent(s.name)}`, {
        method: "PATCH",
        body,
      });
      toast.success("Health check saved", {
        description: "Redeploying to apply it.",
      });
      onSaved();
      onClose();
    } catch (e) {
      toast.error(errorMessage(e));
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <HeartPulse className="size-5" /> Health check for {s.name}
          </DialogTitle>
          <DialogDescription>
            Images with a Docker HEALTHCHECK report their own health. Otherwise
            the agent can request a URL on the service: any response below 400
            counts as healthy.
          </DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-[1fr_8rem] gap-3">
          <div className="space-y-2">
            <Label htmlFor="hc-path">Path</Label>
            <Input
              id="hc-path"
              className="font-mono"
              value={path}
              onChange={(e) => setPath(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="hc-int">Every (s)</Label>
            <Input
              id="hc-int"
              type="number"
              min={5}
              value={interval}
              onChange={(e) => setInterval(e.target.value)}
            />
          </div>
        </div>
        {!s.port && (
          <p className="text-sm text-[oklch(0.55_0.13_70)] dark:text-warning">
            Set a port for this service first.
          </p>
        )}
        <DialogFooter>
          {current && (
            <Button
              variant="outline"
              onClick={() => save({ clear_health_check: true })}
              disabled={saving}
            >
              Remove
            </Button>
          )}
          <Button
            onClick={() =>
              save({
                health_check: { path, interval_seconds: Number(interval) },
              })
            }
            disabled={saving || !s.port}
          >
            {saving && <Loader2 className="animate-spin" />} Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
