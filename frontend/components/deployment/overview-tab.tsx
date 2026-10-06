"use client";

import { useState } from "react";
import { Cpu, Loader2, MemoryStick, Network, Pencil } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, Service } from "@/lib/types";
import {
  deploymentSource,
  formatBytes,
  timeAgo,
  typeLabels,
} from "@/lib/format";
import { ContainerState, HealthBadge } from "@/components/status";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export function OverviewTab({
  d,
  onChanged,
}: {
  d: Deployment;
  onChanged: () => void;
}) {
  const [editingImage, setEditingImage] = useState(false);
  const rev = d.revision;
  const running = d.services.filter((s) => s.state === "running").length;

  async function setAutoDeploy(v: boolean) {
    try {
      await api(`/deployments/${d.id}`, {
        method: "PATCH",
        body: { auto_deploy: v },
      });
      toast.success(v ? "Auto deploy on" : "Auto deploy off");
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <div className="space-y-6">
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2.5 text-sm">
              <dt className="text-muted-foreground">Type</dt>
              <dd>{typeLabels[d.type]}</dd>
              <dt className="text-muted-foreground">
                {d.type === "IMAGE" ? "Image" : "Source"}
              </dt>
              <dd className="flex min-w-0 items-center gap-2 font-mono text-xs">
                <span className="truncate">{deploymentSource(d)}</span>
                {d.type === "IMAGE" && (
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => setEditingImage(true)}
                    aria-label="Change image"
                  >
                    <Pencil />
                  </Button>
                )}
              </dd>
              {rev?.image_digest && d.type === "IMAGE" && (
                <>
                  <dt className="text-muted-foreground">Digest</dt>
                  <dd
                    className="truncate font-mono text-xs"
                    title={rev.image_digest}
                  >
                    {rev.image_digest.slice(0, 19)}…
                  </dd>
                </>
              )}
              {rev?.git_commit && (
                <>
                  <dt className="text-muted-foreground">Commit</dt>
                  <dd className="font-mono text-xs">
                    {rev.git_commit.slice(0, 7)}
                  </dd>
                </>
              )}
              <dt className="text-muted-foreground">Machine</dt>
              <dd>
                {d.agent_name}{" "}
                <span className="text-muted-foreground">
                  ({d.agent_status.toLowerCase()})
                </span>
              </dd>
              {d.type !== "COMPOSE" && (
                <>
                  <dt className="text-muted-foreground">Port</dt>
                  <dd className="font-mono">{d.services[0]?.port ?? "—"}</dd>
                </>
              )}
              <dt className="text-muted-foreground">Services</dt>
              <dd>
                {running} of {d.services.length} running
              </dd>
              {d.type !== "COMPOSE" &&
              (d.spec.cpus || d.spec.memory_mb || d.spec.volumes?.length) ? (
                <>
                  <dt className="text-muted-foreground">Resources</dt>
                  <dd className="text-xs">
                    {[
                      d.spec.cpus && `${d.spec.cpus} CPU`,
                      d.spec.memory_mb && `${d.spec.memory_mb} MB`,
                      d.spec.restart_policy &&
                        `restart ${d.spec.restart_policy}`,
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                    {d.spec.volumes?.map((v) => (
                      <div key={v.target} className="font-mono">
                        {v.source} → {v.target}
                      </div>
                    ))}
                  </dd>
                </>
              ) : null}
              <dt className="text-muted-foreground">Current version</dt>
              <dd>
                {rev
                  ? `#${rev.number} · ${rev.trigger}${rev.rollback_of ? ` of #${rev.rollback_of}` : ""} · ${timeAgo(rev.created_at)}`
                  : "—"}
              </dd>
              <dt className="text-muted-foreground">Created</dt>
              <dd>{new Date(d.created_at).toLocaleString()}</dd>
            </dl>
            {d.spec.source?.kind === "git" && (
              <label className="mt-4 flex items-start gap-3 rounded-lg border p-3">
                <Switch
                  checked={d.auto_deploy}
                  onCheckedChange={setAutoDeploy}
                  className="mt-0.5"
                />
                <span>
                  <span className="block text-sm font-medium">Auto deploy</span>
                  <span className="block text-xs text-muted-foreground">
                    Redeploy when {d.spec.source.git_branch} gets new commits.
                  </span>
                </span>
              </label>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Resource usage</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {d.services.map((s) => (
              <ServiceStats key={s.id} s={s} showName={d.services.length > 1} />
            ))}
            <p className="text-xs text-muted-foreground">
              Current values, updated every 10 seconds by the agent.
            </p>
          </CardContent>
        </Card>
      </div>
      {editingImage && (
        <ChangeImageDialog
          d={d}
          onClose={() => setEditingImage(false)}
          onSaved={onChanged}
        />
      )}
    </div>
  );
}

function ServiceStats({ s, showName }: { s: Service; showName: boolean }) {
  const st = s.stats;
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        {showName ? (
          <span className="text-sm font-medium">{s.name}</span>
        ) : (
          <span />
        )}
        <span className="flex items-center gap-2">
          <ContainerState state={s.state} />
          <HealthBadge health={s.health} />
        </span>
      </div>
      {st ? (
        <div className="grid grid-cols-3 gap-2 text-sm">
          <Metric
            icon={Cpu}
            label="CPU"
            value={`${st.cpu_percent.toFixed(1)}%`}
          />
          <Metric
            icon={MemoryStick}
            label="Memory"
            value={formatBytes(st.memory_bytes)}
            sub={
              st.memory_limit ? `of ${formatBytes(st.memory_limit)}` : undefined
            }
          />
          <Metric
            icon={Network}
            label="Network"
            value={`↓ ${formatBytes(st.net_rx_bytes)}`}
            sub={`↑ ${formatBytes(st.net_tx_bytes)}`}
          />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">
          {s.state === "running"
            ? "Waiting for the next report…"
            : "Not running."}
        </p>
      )}
    </div>
  );
}

function Metric({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: typeof Cpu;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="rounded-lg border p-2.5">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" /> {label}
      </div>
      <div className="mt-1 font-medium tabular-nums">{value}</div>
      {sub && (
        <div className="text-xs text-muted-foreground tabular-nums">{sub}</div>
      )}
    </div>
  );
}

function ChangeImageDialog({
  d,
  onClose,
  onSaved,
}: {
  d: Deployment;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [image, setImage] = useState(d.spec.image ?? "");
  const [saving, setSaving] = useState(false);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      await api(`/deployments/${d.id}`, {
        method: "PATCH",
        body: { spec: { ...d.spec, image: image.trim() }, deploy: true },
      });
      toast.success(`Deploying ${image}…`);
      onSaved();
      onClose();
    } catch (err) {
      toast.error(errorMessage(err));
      setSaving(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={save} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Change image</DialogTitle>
            <DialogDescription>
              Deploys a new version with this image. The public URL stays the
              same.
            </DialogDescription>
          </DialogHeader>
          <Input
            autoFocus
            className="font-mono"
            value={image}
            onChange={(e) => setImage(e.target.value)}
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving || !image.trim()}>
              {saving && <Loader2 className="animate-spin" />} Save and deploy
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
