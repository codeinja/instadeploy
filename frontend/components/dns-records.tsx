import type { DNSRecord } from "@/lib/types";
import { CopyButton } from "@/components/copy-button";
import { Badge } from "@/components/ui/badge";

// One block per record. DNS names and values are long, so they wrap
// instead of being squeezed into table columns.
export function DnsRecords({ records }: { records: DNSRecord[] }) {
  if (records.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Pangolin didn&apos;t return any DNS records for this domain.
      </p>
    );
  }
  return (
    <div className="min-w-0 space-y-2">
      {records.map((r) => (
        <div
          key={r.type + r.name + r.value}
          className="min-w-0 space-y-2 rounded-lg border p-3"
        >
          <Badge variant="secondary" className="font-mono">
            {r.type}
          </Badge>
          <Field label="Name" value={r.name} />
          <Field label="Value" value={r.value} />
        </div>
      ))}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[3.5rem_1fr_auto] items-center gap-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <code className="min-w-0 rounded bg-muted px-2 py-1 font-mono text-xs break-all">
        {value}
      </code>
      <CopyButton value={value} label={`Copy ${label.toLowerCase()}`} />
    </div>
  );
}
