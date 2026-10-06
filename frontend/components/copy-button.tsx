"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";

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
      await navigator.clipboard.writeText(value);
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
