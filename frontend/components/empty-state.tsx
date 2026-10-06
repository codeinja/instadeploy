import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon: LucideIcon;
  title: string;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-4 rounded-xl border border-dashed bg-card/50 px-6 py-14 text-center",
        className,
      )}
    >
      <div className="relative">
        <div className="absolute inset-0 -m-2 rounded-2xl bg-primary/10 blur-md" />
        <div className="relative flex size-12 items-center justify-center rounded-xl border bg-card shadow-sm">
          <Icon className="size-5 text-primary" />
        </div>
      </div>
      <div className="space-y-1">
        <p className="font-semibold">{title}</p>
        {description && (
          <p className="mx-auto max-w-sm text-sm text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {action}
    </div>
  );
}
