"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";

// navigator.clipboard only exists on HTTPS pages and localhost, but the
// dashboard is often opened over plain http on a LAN address. Fall back to
// selecting a hidden textarea and running the old copy command.
async function copyText(text: string) {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Permission denied or blocked: try the fallback.
    }
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.cssText = "position:fixed;top:0;left:0;opacity:0;";
  // Inside the focused element, so it also works within open dialogs, which
  // trap focus and would reject a textarea added to <body>.
  (document.activeElement?.closest("[role=dialog]") ?? document.body).appendChild(area);
  area.select();
  area.setSelectionRange(0, text.length);
  const ok = document.execCommand("copy");
  area.remove();
  if (!ok) throw new Error("copy failed");
}

export function CopyButton({
  value,
  label = "Copy",
  className,
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await copyText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Could not copy. Select the text and copy it manually.");
    }
  }
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      onClick={copy}
      className={className}
      aria-label={label}
      title={label}
    >
      {copied ? <Check /> : <Copy />}
    </Button>
  );
}
