"use client";

import {
  Box,
  Check,
  ExternalLink,
  FileCode2,
  Layers,
  Loader2,
  X,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { CopyButton } from "@/components/copy-button";
import type {
  AgentStatus,
  Deployment,
  DeploymentStatus,
  DeploymentType,
  Health,
  Route,
  Service,
} from "@/lib/types";

type Tone = "success" | "info" | "warning" | "danger" | "neutral";

const toneClass: Record<Tone, string> = {
  success: "bg-success/12 text-success ring-success/25",
  info: "bg-info/12 text-info ring-info/25",
  warning:
    "bg-warning/15 text-[oklch(0.5_0.12_70)] ring-warning/30 dark:text-warning",
  danger: "bg-destructive/10 text-destructive ring-destructive/25",
  neutral: "bg-muted text-muted-foreground ring-border",
};

const statusMeta: Record<
  DeploymentStatus | AgentStatus,
  { label: string; tone: Tone; live?: boolean }
> = {
  RUNNING: { label: "Running", tone: "success" },
  ONLINE: { label: "Online", tone: "success" },
  QUEUED: { label: "Queued", tone: "neutral", live: true },
  BUILDING: { label: "Building", tone: "info", live: true },
  DEPLOYING: { label: "Deploying", tone: "info", live: true },
  DELETING: { label: "Deleting", tone: "warning", live: true },
  STOPPED: { label: "Stopped", tone: "neutral" },
  OFFLINE: { label: "Offline", tone: "neutral" },
  REVOKED: { label: "Revoked", tone: "danger" },
  FAILED: { label: "Failed", tone: "danger" },
};

export function StatusBadge({
  status,
  className,
}: {
  status: DeploymentStatus | AgentStatus;
  className?: string;
}) {
  const meta = statusMeta[status] ?? { label: status, tone: "neutral" as Tone };
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        toneClass[meta.tone],
        className,
      )}
    >
      <span className="relative flex size-1.5">
        {meta.live && (
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-60" />
        )}
        <span className="relative inline-flex size-1.5 rounded-full bg-current" />
      </span>
      {meta.label}
    </span>
  );
}

const healthMeta: Record<Health, { label: string; tone: Tone }> = {
  HEALTHY: { label: "Healthy", tone: "success" },
  UNHEALTHY: { label: "Unhealthy", tone: "danger" },
  STARTING: { label: "Starting", tone: "warning" },
  NONE: { label: "No health check", tone: "neutral" },
};

export function HealthBadge({ health }: { health: Health }) {
  const meta = healthMeta[health];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs ring-1 ring-inset",
        toneClass[meta.tone],
      )}
    >
      <span className="size-1.5 rounded-full bg-current" />
      {meta.label}
    </span>
  );
}

export function ContainerState({ state }: { state: string }) {
  if (!state) return <span className="text-xs text-muted-foreground">—</span>;
  const tone: Tone =
    state === "running"
      ? "success"
      : state === "restarting"
        ? "warning"
        : state === "exited"
          ? "neutral"
          : "danger";
  return (
    <span
      className={cn(
        "rounded-full px-2 py-0.5 text-xs capitalize ring-1 ring-inset",
        toneClass[tone],
      )}
    >
      {state}
    </span>
  );
}

const typeIcons: Record<DeploymentType, typeof Box> = {
  IMAGE: Box,
  DOCKERFILE: FileCode2,
  COMPOSE: Layers,
};

// A small tile showing what kind of deployment this is.
export function TypeIcon({
  type,
  className,
}: {
  type: DeploymentType;
  className?: string;
}) {
  const Icon = typeIcons[type] ?? Box;
  return (
    <span
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-foreground",
        className,
      )}
    >
      <Icon className="size-4" />
    </span>
  );
}

// One sentence explaining where a deployment is at, when the badge alone
// doesn't say enough.
export function statusHint(d: Deployment): string | null {
  const offline = d.agent_status !== "ONLINE";
  if (d.status === "QUEUED" && offline)
    return `This machine (${d.agent_name}) is currently offline. The deployment will start when it reconnects.`;
  if (d.status === "QUEUED") return "Waiting for the machine to pick this up…";
  if (d.status === "BUILDING")
    return d.type === "IMAGE" ? "Pulling the image…" : "Building…";
  if (d.status === "DEPLOYING") return "Starting containers…";
  if (d.status === "DELETING" && offline)
    return `This machine (${d.agent_name}) is currently offline. The containers will be removed when it reconnects.`;
  if (d.status === "DELETING") return "Removing containers…";
  if (d.pending_action && d.status === "RUNNING")
    return `${d.pending_action.toLowerCase()} requested…`;
  return null;
}

const steps = ["QUEUED", "BUILDING", "DEPLOYING", "RUNNING"] as const;
const stepLabel = {
  QUEUED: "Queued",
  BUILDING: "Building",
  DEPLOYING: "Starting",
  RUNNING: "Live",
};

// Where a deployment is: Queued → Building → Starting → Live.
export function DeployProgress({ d }: { d: Deployment }) {
  const current = steps.indexOf(d.status as (typeof steps)[number]);
  const failed = d.status === "FAILED";
  const failedAt = failed ? (d.revision?.status === "DEPLOYING" ? 2 : 1) : -1;
  return (
    <ol className="flex items-center gap-2">
      {steps.map((step, i) => {
        const done = !failed && current > i;
        const active = !failed && current === i && step !== "RUNNING";
        const live = !failed && step === "RUNNING" && current === 3;
        const error = failed && i === failedAt;
        const reached = done || live || (failed && i < failedAt);
        return (
          <li
            key={step}
            className={cn(
              "flex items-center gap-2",
              i < steps.length - 1 && "flex-1",
            )}
          >
            <span
              className={cn(
                "flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-medium transition-colors",
                reached && "border-success bg-success text-white",
                active && "border-primary bg-primary/10 text-primary",
                error && "border-destructive bg-destructive text-white",
                !reached &&
                  !active &&
                  !error &&
                  "bg-background text-muted-foreground",
              )}
            >
              {reached ? (
                <Check className="size-3.5" />
              ) : error ? (
                <X className="size-3.5" />
              ) : active ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                i + 1
              )}
            </span>
            <span
              className={cn(
                "hidden text-xs font-medium whitespace-nowrap sm:inline",
                active || error || live
                  ? "text-foreground"
                  : "text-muted-foreground",
              )}
            >
              {stepLabel[step]}
            </span>
            {i < steps.length - 1 && (
              <span
                className={cn(
                  "h-px min-w-4 flex-1",
                  reached ? "bg-success" : "bg-border",
                )}
              />
            )}
          </li>
        );
      })}
    </ol>
  );
}

// A public URL as a chip: open on click, copy with the button.
export function UrlChip({
  url,
  className,
  size = "sm",
}: {
  url: string;
  className?: string;
  size?: "sm" | "lg";
}) {
  return (
    <span
      className={cn(
        "group/url inline-flex max-w-full items-center gap-1.5 rounded-lg border bg-card pr-0.5 pl-2.5 shadow-xs",
        size === "lg" ? "h-9 text-sm" : "h-7 text-xs",
        className,
      )}
    >
      <span className="size-1.5 shrink-0 rounded-full bg-success" />
      <a
        href={url}
        target="_blank"
        rel="noreferrer"
        className="flex min-w-0 items-center gap-1 font-mono text-foreground hover:text-primary"
      >
        <span className="truncate">{url.replace(/^https:\/\//, "")}</span>
        <ExternalLink className="size-3 shrink-0 opacity-50 group-hover/url:opacity-100" />
      </a>
      <CopyButton
        value={url}
        label="Copy URL"
        className="size-6 opacity-60 hover:opacity-100"
      />
    </span>
  );
}

export function RouteLink({
  route,
  className,
}: {
  route: Route;
  className?: string;
}) {
  if (route.status === "READY")
    return <UrlChip url={route.url} className={className} />;
  if (route.status === "FAILED")
    return (
      <span className={cn("text-sm text-destructive", className)}>
        Public URL failed
      </span>
    );
  if (route.status === "DISABLED") {
    return (
      <span className={cn("text-sm text-muted-foreground", className)}>
        No public URL yet (Pangolin isn&apos;t connected)
      </span>
    );
  }
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm text-muted-foreground",
        className,
      )}
    >
      <Loader2 className="size-3.5 animate-spin" /> {route.hostname}
    </span>
  );
}

// The main public URL(s) of a deployment, compact.
export function DeploymentUrls({
  d,
  max = 2,
}: {
  d: Deployment;
  max?: number;
}) {
  const routes = d.services.flatMap((s) => s.routes.map((r) => ({ s, r })));
  if (routes.length === 0) {
    const anyPublic = d.services.some((s: Service) => s.public);
    return (
      <span className="text-sm text-muted-foreground">
        {anyPublic ? "—" : "Private"}
      </span>
    );
  }
  routes.sort((a, b) =>
    a.r.kind === b.r.kind ? 0 : a.r.kind === "CUSTOM" ? -1 : 1,
  );
  return (
    <div className="flex min-w-0 flex-col items-start gap-1">
      {routes.slice(0, max).map(({ r }) => (
        <RouteLink key={r.id} route={r} className="max-w-full" />
      ))}
      {routes.length > max && (
        <span className="text-xs text-muted-foreground">
          +{routes.length - max} more
        </span>
      )}
    </div>
  );
}
