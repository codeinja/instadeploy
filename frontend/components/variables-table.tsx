"use client";

import { useCallback, useState } from "react";
import {
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Variable } from "@/lib/types";
import { usePoll } from "@/lib/use-poll";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { SimpleSelect } from "@/components/simple-select";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";

type Scope =
  { projectId: string } | { deploymentId: string; services: string[] };

const envOptions = [
  { value: "", label: "All environments" },
  { value: "development", label: "Development" },
  { value: "staging", label: "Staging" },
  { value: "production", label: "Production" },
];

// Saved variables and secrets for a project or a deployment. Values are
// masked until revealed; secret values can't be read back at all.
export function VariablesTable({
  scope,
  onChanged,
}: {
  scope: Scope;
  onChanged?: () => void;
}) {
  const query =
    "projectId" in scope
      ? `project_id=${scope.projectId}`
      : `deployment_id=${scope.deploymentId}`;
  const load = useCallback(
    () => api<Variable[]>(`/variables?${query}`),
    [query],
  );
  const { data, error, loading, refresh } = usePoll(load, 15000);
  const [revealed, setRevealed] = useState<Record<string, boolean>>({});
  const [editing, setEditing] = useState<Variable | "new" | null>(null);
  const [deleting, setDeleting] = useState<Variable | null>(null);

  const isProject = "projectId" in scope;

  async function remove(v: Variable) {
    try {
      await api(`/variables/${v.id}`, { method: "DELETE" });
      toast.success(`Removed ${v.key}`);
      refresh();
      onChanged?.();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <div className="space-y-3">
      {error && <ErrorState message={error} onRetry={refresh} />}
      {loading ? (
        <Skeleton className="h-24 w-full" />
      ) : data && data.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[30%]">Key</TableHead>
                <TableHead>Value</TableHead>
                <TableHead className="w-40">
                  {isProject ? "Environment" : "Service"}
                </TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((v) => (
                <TableRow key={v.id}>
                  <TableCell className="font-mono text-sm">
                    <span className="flex items-center gap-2">
                      {v.key}
                      {v.secret && (
                        <Badge variant="secondary" className="gap-1">
                          <KeyRound className="size-3" /> secret
                        </Badge>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="max-w-0 font-mono text-sm">
                    {v.secret ? (
                      <span className="text-muted-foreground">
                        ••••••••••••
                      </span>
                    ) : (
                      <span className="flex items-center gap-2">
                        <span className="truncate">
                          {revealed[v.id] ? v.value : "••••••••"}
                        </span>
                        <button
                          type="button"
                          className="text-muted-foreground hover:text-foreground"
                          onClick={() =>
                            setRevealed({
                              ...revealed,
                              [v.id]: !revealed[v.id],
                            })
                          }
                          aria-label={
                            revealed[v.id] ? "Hide value" : "Show value"
                          }
                        >
                          {revealed[v.id] ? (
                            <EyeOff className="size-4" />
                          ) : (
                            <Eye className="size-4" />
                          )}
                        </button>
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {isProject
                      ? (v.environment ?? "All")
                      : (v.service ?? "All services")}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setEditing(v)}
                      aria-label={`Edit ${v.key}`}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setDeleting(v)}
                      aria-label={`Delete ${v.key}`}
                    >
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          No variables yet. Add things like <code>DATABASE_URL</code>,{" "}
          <code>API_KEY</code> or <code>NODE_ENV</code>.
        </p>
      )}
      <Button variant="outline" size="sm" onClick={() => setEditing("new")}>
        <Plus /> Add variable
      </Button>

      {editing && (
        <VariableDialog
          scope={scope}
          variable={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            refresh();
            onChanged?.();
          }}
        />
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete ${deleting?.key}?`}
        description="Running containers keep the old value until the next deploy."
        confirmLabel="Delete"
        onConfirm={() => deleting && remove(deleting)}
      />
    </div>
  );
}

function VariableDialog({
  scope,
  variable,
  onClose,
  onSaved,
}: {
  scope: Scope;
  variable: Variable | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const isProject = "projectId" in scope;
  const [key, setKey] = useState(variable?.key ?? "");
  const [value, setValue] = useState(
    variable && !variable.secret ? variable.value : "",
  );
  const [secret, setSecret] = useState(variable?.secret ?? false);
  const [environment, setEnvironment] = useState(variable?.environment ?? "");
  const [service, setService] = useState(variable?.service ?? "");
  const [saving, setSaving] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      if (variable) {
        const body: Record<string, unknown> = { secret };
        // Leaving a secret's value empty keeps the current one.
        if (!(variable.secret && value === "")) body.value = value;
        await api(`/variables/${variable.id}`, { method: "PATCH", body });
      } else {
        await api("/variables", {
          method: "POST",
          body: {
            ...(isProject
              ? { project_id: scope.projectId, environment }
              : { deployment_id: scope.deploymentId, service }),
            key,
            value,
            secret,
          },
        });
      }
      toast.success(variable ? `Updated ${key}` : `Added ${key}`, {
        description: "Redeploy to apply the change.",
      });
      onSaved();
    } catch (e) {
      toast.error(errorMessage(e));
      setSaving(false);
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={save} className="space-y-4">
          <DialogHeader>
            <DialogTitle>
              {variable ? `Edit ${variable.key}` : "Add variable"}
            </DialogTitle>
            <DialogDescription>
              Available to containers as an environment variable on the next
              deploy.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="var-key">Key</Label>
            <Input
              id="var-key"
              required
              disabled={!!variable}
              className="font-mono"
              placeholder="DATABASE_URL"
              value={key}
              onChange={(e) =>
                setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "_"))
              }
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="var-value">Value</Label>
            <Textarea
              id="var-value"
              rows={3}
              className="font-mono"
              autoComplete="off"
              placeholder={
                variable?.secret ? "Leave empty to keep the current secret" : ""
              }
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </div>
          {!variable && isProject && (
            <div className="space-y-2">
              <Label>Environment</Label>
              <SimpleSelect
                value={environment}
                onChange={setEnvironment}
                options={envOptions}
              />
            </div>
          )}
          {!variable && !isProject && scope.services.length > 1 && (
            <div className="space-y-2">
              <Label>Service</Label>
              <SimpleSelect
                value={service}
                onChange={setService}
                options={[
                  { value: "", label: "All services" },
                  ...scope.services.map((s) => ({ value: s, label: s })),
                ]}
              />
            </div>
          )}
          <label className="flex items-start gap-3 rounded-lg border p-3">
            <Switch
              checked={secret}
              onCheckedChange={setSecret}
              className="mt-0.5"
            />
            <span className="space-y-0.5">
              <span className="block text-sm font-medium">Secret</span>
              <span className="block text-xs text-muted-foreground">
                Encrypted, never shown again, and redacted from logs.
              </span>
            </span>
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving || !key}>
              {saving && <Loader2 className="animate-spin" />} Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
