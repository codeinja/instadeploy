"use client";

import { Eye, EyeOff, KeyRound, Plus, X } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { SimpleSelect } from "@/components/simple-select";

export interface EnvRow {
  key: string;
  value: string;
  secret: boolean;
  service?: string;
}

// Rows of KEY / value / secret, used in the deploy form before anything is
// saved. (Saved variables are edited with VariablesTable.)
export function EnvEditor({
  rows,
  onChange,
  services,
}: {
  rows: EnvRow[];
  onChange: (rows: EnvRow[]) => void;
  // For Compose: lets a variable target one service.
  services?: string[];
}) {
  const [shown, setShown] = useState<Record<number, boolean>>({});
  const update = (i: number, patch: Partial<EnvRow>) =>
    onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  return (
    <div className="space-y-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-wrap items-center gap-2 sm:flex-nowrap"
        >
          <Input
            placeholder="KEY"
            className="font-mono sm:w-48"
            value={row.key}
            onChange={(e) =>
              update(i, {
                key: e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "_"),
              })
            }
          />
          <div className="relative flex-1">
            <Input
              placeholder="value"
              className="pr-9 font-mono"
              type={row.secret && !shown[i] ? "password" : "text"}
              autoComplete="off"
              value={row.value}
              onChange={(e) => update(i, { value: e.target.value })}
            />
            {row.secret && (
              <button
                type="button"
                className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                onClick={() => setShown({ ...shown, [i]: !shown[i] })}
                aria-label={shown[i] ? "Hide value" : "Show value"}
              >
                {shown[i] ? (
                  <EyeOff className="size-4" />
                ) : (
                  <Eye className="size-4" />
                )}
              </button>
            )}
          </div>
          {services && services.length > 1 && (
            <SimpleSelect
              className="sm:w-36"
              value={row.service ?? ""}
              onChange={(v) => update(i, { service: v || undefined })}
              options={[
                { value: "", label: "All services" },
                ...services.map((s) => ({ value: s, label: s })),
              ]}
            />
          )}
          <label
            className="flex items-center gap-1.5 text-xs text-muted-foreground"
            title="Secrets are encrypted and never shown again"
          >
            <Switch
              checked={row.secret}
              onCheckedChange={(v) => update(i, { secret: v })}
              size="sm"
            />
            <KeyRound className="size-3.5" /> Secret
          </label>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
            aria-label="Remove"
          >
            <X />
          </Button>
        </div>
      ))}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() =>
          onChange([...rows, { key: "", value: "", secret: false }])
        }
      >
        <Plus /> Add variable
      </Button>
    </div>
  );
}
