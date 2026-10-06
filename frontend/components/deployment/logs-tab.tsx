"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, LogLine } from "@/lib/types";
import { LogViewer } from "@/components/log-viewer";
import { SimpleSelect } from "@/components/simple-select";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

// Build and deployment logs stream live (Server-Sent Events); container
// logs are fetched from the machine on demand.
export function LogsTab({ d }: { d: Deployment }) {
  const [view, setView] = useState<"build" | "deploy" | "container">(
    d.status === "RUNNING" ? "deploy" : "build",
  );
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    // Lines arrive in order with increasing ids; EventSource resumes with
    // Last-Event-ID after a reconnect, so nothing is duplicated.
    const es = new EventSource(`/api/deployments/${d.id}/logs/stream`);
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (ev) => {
      const line = JSON.parse(ev.data) as LogLine;
      setLines((prev) =>
        prev.length && prev[prev.length - 1].id >= line.id
          ? prev
          : [...prev, line],
      );
    };
    return () => es.close();
  }, [d.id]);

  const shown = useMemo(
    () =>
      lines
        .filter((l) => l.stream === view)
        .map((l) => ({
          key: l.id,
          text: l.line,
          tone: /^(Error|Deployment failed|Container started, but)/.test(l.line)
            ? ("error" as const)
            : /^(Deployment is running|Pangolin route created|Container running|Build complete|All services started)/.test(
                  l.line,
                )
              ? ("success" as const)
              : l.line.startsWith("Deploying ")
                ? ("muted" as const)
                : undefined,
        })),
    [lines, view],
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs value={view} onValueChange={(v) => setView(v as typeof view)}>
          <TabsList>
            <TabsTrigger value="build" className="px-3">
              Build
            </TabsTrigger>
            <TabsTrigger value="deploy" className="px-3">
              Deployment
            </TabsTrigger>
            <TabsTrigger value="container" className="px-3">
              Container
            </TabsTrigger>
          </TabsList>
        </Tabs>
        {view !== "container" && (
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="relative flex size-2">
              {connected && (
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-success opacity-60" />
              )}
              <span
                className={`relative inline-flex size-2 rounded-full ${connected ? "bg-success" : "bg-muted-foreground/50"}`}
              />
            </span>
            {connected ? "Live" : "Reconnecting…"}
          </span>
        )}
      </div>
      {view === "container" ? (
        <ContainerLogs d={d} />
      ) : (
        <LogViewer
          lines={shown}
          empty={
            view === "build"
              ? d.type === "IMAGE"
                ? "Docker image deployments don't build anything; see Deployment logs."
                : "No build output yet."
              : "No deployment logs yet."
          }
        />
      )}
    </div>
  );
}

function ContainerLogs({ d }: { d: Deployment }) {
  const [service, setService] = useState(
    d.services.find((s) => s.public)?.name ?? d.services[0]?.name ?? "",
  );
  const [lines, setLines] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [auto, setAuto] = useState(false);

  const load = useCallback(async () => {
    if (!service) return;
    setLoading(true);
    try {
      const res = await api<{ lines: string[] }>(
        `/deployments/${d.id}/services/${encodeURIComponent(service)}/logs?tail=300`,
      );
      setLines(res.lines);
      setError(null);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [d.id, service]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch when the service changes
    load();
  }, [load]);
  useEffect(() => {
    if (!auto) return;
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [auto, load]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        {d.services.length > 1 && (
          <SimpleSelect
            className="w-48"
            value={service}
            onChange={setService}
            options={d.services.map((s) => ({ value: s.name, label: s.name }))}
          />
        )}
        <Button variant="outline" size="sm" onClick={load} disabled={loading}>
          {loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}{" "}
          Refresh
        </Button>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={auto} onCheckedChange={setAuto} size="sm" /> Auto
          refresh
        </label>
        <span className="text-xs text-muted-foreground">
          Last 300 lines, like docker logs. Secrets are redacted.
        </span>
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}
      <LogViewer
        lines={(lines ?? []).map((text, i) => {
          // "2026-10-06T18:30:12.123456789Z message" -> "2026-10-06 18:30:12 message"
          const m = text.match(
            /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})\.\d+Z ([\s\S]*)$/,
          );
          return { key: i, text: m ? `${m[1]} ${m[2]} ${m[3]}` : text };
        })}
        empty={
          lines === null
            ? "Loading…"
            : "The container hasn't written any output."
        }
      />
    </div>
  );
}
