import Link from "next/link";
import { CheckCircle2, Info, XCircle } from "lucide-react";
import type { ActivityEvent } from "@/lib/types";
import { timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";

const levels = {
  info: { icon: Info, className: "text-muted-foreground bg-muted" },
  success: { icon: CheckCircle2, className: "text-success bg-success/12" },
  error: { icon: XCircle, className: "text-destructive bg-destructive/10" },
};

// One entry of the activity timeline.
export function ActivityItem({
  event: e,
  last,
  compact,
}: {
  event: ActivityEvent;
  last?: boolean;
  compact?: boolean;
}) {
  const { icon: Icon, className } = levels[e.level] ?? levels.info;
  const message = (
    <span className={cn("text-sm", e.level === "error" && "text-destructive")}>
      {e.message}
    </span>
  );
  return (
    <li className="relative flex gap-3 pb-4 last:pb-0">
      {!last && (
        <span className="absolute top-7 bottom-0 left-3 w-px bg-border" />
      )}
      <span
        className={cn(
          "relative z-10 flex size-6 shrink-0 items-center justify-center rounded-full",
          className,
        )}
      >
        <Icon className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        {e.deployment_id ? (
          <Link
            href={`/deployments/${e.deployment_id}`}
            className={cn("hover:underline", compact && "line-clamp-2")}
          >
            {message}
          </Link>
        ) : (
          <p className={cn(compact && "line-clamp-2")}>{message}</p>
        )}
        <time
          className="text-xs text-muted-foreground"
          title={new Date(e.created_at).toLocaleString()}
        >
          {timeAgo(e.created_at)}
        </time>
      </div>
    </li>
  );
}
