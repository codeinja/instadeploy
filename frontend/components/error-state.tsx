import { TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";

export function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-xl border border-destructive/25 bg-destructive/5 p-4 text-sm"
    >
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="flex-1 space-y-2">
        <p className="text-destructive">{message}</p>
        {onRetry && (
          <Button size="sm" variant="outline" onClick={onRetry}>
            Try again
          </Button>
        )}
      </div>
    </div>
  );
}
