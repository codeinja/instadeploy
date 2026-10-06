"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

export interface Option {
  value: string;
  label: React.ReactNode;
  disabled?: boolean;
}

// A plain select: a list of options, a value, a callback.
export function SimpleSelect({
  value,
  onChange,
  options,
  placeholder = "Select…",
  className,
  id,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: Option[];
  placeholder?: string;
  className?: string;
  id?: string;
  disabled?: boolean;
}) {
  return (
    <Select
      items={options.map((o) => ({ value: o.value, label: o.label }))}
      value={value || null}
      onValueChange={(v) => v != null && onChange(String(v))}
      disabled={disabled}
    >
      <SelectTrigger id={id} className={cn("w-full", className)}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value} disabled={o.disabled}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
