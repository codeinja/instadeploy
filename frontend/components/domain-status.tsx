import { Loader2, Lock, LockOpen } from "lucide-react";
import type { Domain } from "@/lib/types";
import { cn } from "@/lib/utils";

// Domain status as one readable line: DNS, then routing, then SSL.
export function DomainStatus({
  domain: d,
  className,
}: {
  domain: Domain;
  className?: string;
}) {
  let text: string;
  let tone: string;
  if (d.status === "PENDING") {
    text = d.error || "Waiting for DNS records";
    tone = "text-[oklch(0.55_0.13_70)] dark:text-warning";
  } else if (d.status === "FAILED") {
    text = d.error || "Failed";
    tone = "text-destructive";
  } else if (!d.service_id) {
    text = "Verified · not pointing anywhere yet";
    tone = "text-muted-foreground";
  } else if (d.route_status === "FAILED") {
    text = d.route_error || "Route failed";
    tone = "text-destructive";
  } else if (d.route_status === "READY") {
    text = "Active";
    tone = "text-success";
  } else {
    text = "Connecting…";
    tone = "text-muted-foreground";
  }
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm",
        tone,
        className,
      )}
    >
      {text === "Connecting…" && <Loader2 className="size-3.5 animate-spin" />}
      {text}
      {d.status === "ACTIVE" && d.route_status === "READY" && (
        <span
          className="inline-flex items-center gap-1 text-xs text-muted-foreground"
          title={`SSL: ${d.ssl_status}`}
        >
          {d.ssl_status === "VALID" ? (
            <Lock className="size-3.5 text-success" />
          ) : (
            <LockOpen className="size-3.5" />
          )}
          {d.ssl_status === "VALID" ? "SSL" : "SSL pending"}
        </span>
      )}
    </span>
  );
}
