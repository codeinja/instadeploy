"use client";

import { useCallback, useState } from "react";
import {
  Ban,
  KeyRound,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  Server,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Agent } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { timeAgo } from "@/lib/format";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status";
import { CopyButton } from "@/components/copy-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState } from "@/components/empty-state";
import { PangolinLimits } from "@/components/pangolin-limits";
import { ErrorState } from "@/components/error-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface Connect {
  agent: Agent;
  token: string;
  command: string;
}

export default function AgentsPage() {
  const load = useCallback(() => api<Agent[]>("/agents"), []);
  const { data: agents, error, loading, refresh } = usePoll(load, 4000);
  const [adding, setAdding] = useState(false);
  const [connect, setConnect] = useState<Connect | null>(null);
  const [renaming, setRenaming] = useState<Agent | null>(null);
  const [confirm, setConfirm] = useState<{
    agent: Agent;
    action: "revoke" | "delete" | "reconnect";
  } | null>(null);

  async function run(agent: Agent, action: "revoke" | "delete" | "reconnect") {
    try {
      if (action === "delete") {
        await api(`/agents/${agent.id}`, { method: "DELETE" });
        toast.success(`Deleted ${agent.name}`);
      } else if (action === "revoke") {
        await api(`/agents/${agent.id}/revoke`, { method: "POST" });
        toast.success(`Revoked access for ${agent.name}`);
      } else {
        setConnect(
          await api<Connect>(`/agents/${agent.id}/token`, { method: "POST" }),
        );
      }
      refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const live = connect && agents?.find((a) => a.id === connect.agent.id);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Machines"
        description="Machines that run your deployments. Each one runs a small agent container that connects out to Insta Deploy."
        actions={
          <Button onClick={() => setAdding(true)}>
            <Plus /> Add machine
          </Button>
        }
      />
      {error && <ErrorState message={error} onRetry={refresh} />}
      {!!agents?.length && (
        <PangolinLimits
          usage={{
            machines: agents.filter((a) => a.status !== "REVOKED").length,
          }}
        />
      )}
      {loading ? (
        <div className="grid gap-4 md:grid-cols-2">
          <Skeleton className="h-56 rounded-xl" />
          <Skeleton className="h-56 rounded-xl" />
        </div>
      ) : agents?.length === 0 ? (
        <EmptyState
          icon={Server}
          title="No machines connected"
          description="Connect a laptop, home server or VPS with Docker. It doesn't need a public IP or open ports."
          action={
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus /> Add machine
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {agents?.map((a) => (
            <Card key={a.id} className="gap-0 py-0">
              <div className="flex items-start justify-between gap-3 p-5">
                <div className="flex min-w-0 items-center gap-3">
                  <span
                    className={cn(
                      "flex size-11 shrink-0 items-center justify-center rounded-xl",
                      a.status === "ONLINE"
                        ? "bg-success/12 text-success"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    <Server className="size-5" />
                  </span>
                  <div className="min-w-0 space-y-1">
                    <p className="truncate font-semibold">{a.name}</p>
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge status={a.status} />
                      {a.hostname && (
                        <span className="text-xs text-muted-foreground">
                          seen {timeAgo(a.last_seen)}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Actions for ${a.name}`}
                      />
                    }
                  >
                    <MoreHorizontal />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => setRenaming(a)}>
                      <Pencil /> Rename
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() =>
                        setConfirm({ agent: a, action: "reconnect" })
                      }
                    >
                      <KeyRound />{" "}
                      {a.status === "REVOKED"
                        ? "Reconnect (new token)"
                        : "Issue new token"}
                    </DropdownMenuItem>
                    {a.status !== "REVOKED" && (
                      <DropdownMenuItem
                        onClick={() =>
                          setConfirm({ agent: a, action: "revoke" })
                        }
                      >
                        <Ban /> Revoke access
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setConfirm({ agent: a, action: "delete" })}
                    >
                      <Trash2 /> Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
              <CardContent className="space-y-3 border-t bg-muted/20 p-5">
                {a.hostname ? (
                  <>
                    <div className="grid grid-cols-3 gap-2">
                      <MiniStat label="Deployments" value={a.deployments} />
                      <MiniStat label="Arch" value={a.arch} />
                      <MiniStat label="Docker" value={a.docker_version} />
                    </div>
                    <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1.5 text-sm">
                      <dt className="text-muted-foreground">Hostname</dt>
                      <dd className="truncate font-mono text-xs leading-5">
                        {a.hostname}
                      </dd>
                      <dt className="text-muted-foreground">OS</dt>
                      <dd className="truncate">{a.os}</dd>
                      <dt className="text-muted-foreground">Tunnel</dt>
                      <dd className="flex items-center gap-1.5">
                        <span
                          className={cn(
                            "size-1.5 rounded-full",
                            a.tunnel_ready ? "bg-success" : "bg-warning",
                          )}
                        />
                        {a.tunnel_ready
                          ? "Connected to Pangolin"
                          : "Not configured"}
                      </dd>
                      <dt className="text-muted-foreground">Agent</dt>
                      <dd className="text-muted-foreground">
                        v{a.agent_version}
                      </dd>
                    </dl>
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    Never connected. Run the agent&apos;s docker command on the
                    machine.
                  </p>
                )}
                {a.tunnel_error && !a.tunnel_ready && (
                  <p className="rounded-lg border border-destructive/25 bg-destructive/5 p-2.5 text-xs text-destructive">
                    {a.tunnel_error}
                  </p>
                )}
                {a.status === "REVOKED" && (
                  <p className="flex items-start gap-2 rounded-lg border border-destructive/25 bg-destructive/5 p-2.5 text-xs text-destructive">
                    <ShieldAlert className="size-4 shrink-0" /> This
                    machine&apos;s token was revoked. Its containers keep
                    running, but it can&apos;t receive work until you reconnect
                    it.
                  </p>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <AddMachineDialog
        open={adding}
        onOpenChange={setAdding}
        onCreated={(c) => {
          setConnect(c);
          refresh();
        }}
      />

      <Dialog open={!!connect} onOpenChange={(o) => !o && setConnect(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Connect {connect?.agent.name}</DialogTitle>
            <DialogDescription>
              Run this on the machine with Docker. The token is shown only once.
            </DialogDescription>
          </DialogHeader>
          {connect && (
            <div className="space-y-4">
              <div className="relative">
                <pre className="overflow-x-auto rounded-xl bg-zinc-950 p-4 pr-12 font-mono text-xs leading-relaxed text-zinc-100">
                  {connect.command}
                </pre>
                <CopyButton
                  value={connect.command}
                  className="absolute top-2 right-2 text-zinc-300 hover:bg-zinc-800 hover:text-white"
                />
              </div>
              <div
                className={cn(
                  "flex items-center gap-3 rounded-lg border p-3 text-sm",
                  live?.status === "ONLINE" && "border-success/30 bg-success/5",
                )}
              >
                <StatusBadge status={live?.status ?? "OFFLINE"} />
                {live?.status === "ONLINE" ? (
                  <span className="font-medium text-success">
                    Connected! You can deploy to this machine now.
                  </span>
                ) : (
                  <span className="flex items-center gap-2 text-muted-foreground">
                    <Loader2 className="size-3.5 animate-spin" /> Waiting for
                    the agent to connect…
                  </span>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                The agent mounts <code>/var/run/docker.sock</code>, which gives
                it root-level control of Docker on that machine. Only connect
                machines you trust this Insta Deploy server with.
              </p>
            </div>
          )}
          <DialogFooter>
            <Button onClick={() => setConnect(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {renaming && (
        <RenameDialog
          agent={renaming}
          onClose={() => setRenaming(null)}
          onSaved={refresh}
        />
      )}

      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={
          confirm?.action === "delete"
            ? `Delete ${confirm.agent.name}?`
            : confirm?.action === "revoke"
              ? `Revoke access for ${confirm?.agent.name}?`
              : `Issue a new token for ${confirm?.agent.name}?`
        }
        description={
          confirm?.action === "delete"
            ? `Its ${confirm.agent.deployments} deployment(s) and their public URLs are removed from Insta Deploy. Containers already running on the machine are left alone.`
            : confirm?.action === "revoke"
              ? "The machine is disconnected right away. Its containers keep running, but you can't manage them until you reconnect it."
              : "The current token stops working. You'll get a new docker run command to restart the agent with."
        }
        confirmLabel={
          confirm?.action === "delete"
            ? "Delete"
            : confirm?.action === "revoke"
              ? "Revoke"
              : "Issue new token"
        }
        destructive={confirm?.action !== "reconnect"}
        onConfirm={() => confirm && run(confirm.agent, confirm.action)}
      />
    </div>
  );
}

function AddMachineDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onCreated: (c: Connect) => void;
}) {
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  async function create(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      const c = await api<Connect>("/agents", {
        method: "POST",
        body: { name },
      });
      onOpenChange(false);
      setName("");
      onCreated(c);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={create} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Add Docker machine</DialogTitle>
            <DialogDescription>
              Give it a name you&apos;ll recognize, like &quot;My Laptop&quot;
              or &quot;Home Server&quot;.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="agent-name">Name</Label>
            <Input
              id="agent-name"
              required
              autoFocus
              placeholder="My Laptop"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={saving || !name.trim()}>
              {saving && <Loader2 className="animate-spin" />} Create agent
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({
  agent,
  onClose,
  onSaved,
}: {
  agent: Agent;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(agent.name);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api(`/agents/${agent.id}`, { method: "PATCH", body: { name } });
      toast.success("Renamed");
      onSaved();
      onClose();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={save} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Rename machine</DialogTitle>
          </DialogHeader>
          <Input
            required
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MiniStat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-lg border bg-card px-3 py-2">
      <p className="text-[11px] font-medium text-muted-foreground">{label}</p>
      <p className="truncate text-sm font-semibold tabular-nums">{value}</p>
    </div>
  );
}
