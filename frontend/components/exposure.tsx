import { Globe, Lock, ShieldCheck } from "lucide-react";
import type { Deployment, Service } from "@/lib/types";
import { cn } from "@/lib/utils";

export type Exposure = "private" | "public" | "protected";

export function serviceExposure(
  s: Pick<Service, "public" | "access">,
): Exposure {
  if (!s.public) return "private";
  return s.access && s.access !== "none" ? "protected" : "public";
}

// The most exposed service decides: one unprotected public service makes
// the whole deployment "Public".
export function deploymentExposure(d: Pick<Deployment, "services">): Exposure {
  const all = d.services.map(serviceExposure);
  if (all.includes("public")) return "public";
  if (all.includes("protected")) return "protected";
  return "private";
}

const meta: Record<
  Exposure,
  { label: string; icon: typeof Lock; className: string; title: string }
> = {
  private: {
    label: "Private",
    icon: Lock,
    className: "bg-muted text-muted-foreground ring-border",
    title: "No public URL. Only reachable from inside the project.",
  },
  public: {
    label: "Public",
    icon: Globe,
    className:
      "bg-warning/15 text-[oklch(0.5_0.12_70)] ring-warning/30 dark:text-warning",
    title:
      "Anyone with the URL can reach it. Access depends on the app's own login.",
  },
  protected: {
    label: "Public + Protected",
    icon: ShieldCheck,
    className: "bg-success/12 text-success ring-success/25",
    title: "Public URL behind a password or PIN asked by Pangolin.",
  },
};

export function ExposureBadge({
  exposure,
  className,
}: {
  exposure: Exposure;
  className?: string;
}) {
  const m = meta[exposure];
  return (
    <span
      title={m.title}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset",
        m.className,
        className,
      )}
    >
      <m.icon className="size-3" />
      {m.label}
    </span>
  );
}
