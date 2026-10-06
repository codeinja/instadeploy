import { ChevronDown, Info } from "lucide-react";
import { cn } from "@/lib/utils";

// Pangolin Cloud's free (Basic) plan limits, from pangolin.net/pricing.
// Each machine is a site, each public URL a resource, and the apps domain
// plus every custom domain a domain.
const limits = [
  { key: "machines", label: "Machines", pangolin: "sites", max: 5 },
  { key: "urls", label: "Public URLs", pangolin: "public resources", max: 15 },
  {
    key: "domains",
    label: "Domains",
    pangolin: "domains, incl. the apps domain",
    max: 5,
  },
] as const;

type Usage = Partial<Record<(typeof limits)[number]["key"], number>>;

// A collapsible note about what the free Pangolin Cloud plan allows, with
// current usage where the page knows it.
export function PangolinLimits({
  usage,
  className,
}: {
  usage?: Usage;
  className?: string;
}) {
  const near = limits.some((l) => (usage?.[l.key] ?? 0) >= l.max - 1);
  return (
    <details
      className={cn(
        "group rounded-xl border text-sm",
        near ? "border-warning/40 bg-warning/10" : "border-info/25 bg-info/5",
        className,
      )}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 px-4 py-3 select-none [&::-webkit-details-marker]:hidden">
        <Info
          className={cn(
            "size-4 shrink-0",
            near ? "text-[oklch(0.55_0.13_70)] dark:text-warning" : "text-info",
          )}
        />
        <span className="flex-1 font-medium">
          Using Pangolin Cloud&apos;s free plan?{" "}
          <span className="font-normal text-muted-foreground">
            {near
              ? "You're close to a limit."
              : "It has limits on what you can create."}
          </span>
        </span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
      </summary>
      <div className="space-y-3 border-t border-inherit px-4 py-3">
        <ul className="grid gap-2 sm:grid-cols-3">
          {limits.map((l) => {
            const used = usage?.[l.key];
            const full = used !== undefined && used >= l.max;
            return (
              <li key={l.key} className="rounded-lg border bg-card px-3 py-2">
                <p className="text-xs text-muted-foreground">{l.label}</p>
                <p
                  className={cn(
                    "font-semibold tabular-nums",
                    full && "text-destructive",
                  )}
                >
                  {used !== undefined
                    ? `${used} of ${l.max}`
                    : `Up to ${l.max}`}
                </p>
                <p className="text-[11px] text-muted-foreground">
                  Pangolin {l.pangolin}
                </p>
              </li>
            );
          })}
        </ul>
        <p className="text-xs text-muted-foreground">
          Each machine uses one Pangolin site, and each public URL (generated or
          custom domain) uses one resource. The free plan also allows 5 users
          and 1 organization. When a limit is reached, new machines or public
          URLs fail to connect until you remove something or upgrade. Limits can
          change; check{" "}
          <a
            href="https://pangolin.net/pricing"
            target="_blank"
            rel="noreferrer"
            className="font-medium text-foreground underline underline-offset-4"
          >
            Pangolin&apos;s pricing
          </a>
          . Self-hosted Pangolin has no such limits.
        </p>
      </div>
    </details>
  );
}
