"use client";

import { useState } from "react";
import { cn } from "@/lib/utils";

// App logos come from the selfh.st icon set (served by jsDelivr): SVG if
// there is one, else PNG, else the app's initial.
const formats = ["svg", "png"];

export function AppIcon({
  icon,
  name,
  className,
}: {
  icon: string;
  name: string;
  className?: string;
}) {
  const [attempt, setAttempt] = useState(0);
  if (attempt >= formats.length) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-xl bg-primary/10 font-semibold text-primary",
          className,
        )}
      >
        {name.slice(0, 1)}
      </div>
    );
  }
  return (
    // eslint-disable-next-line @next/next/no-img-element -- small external SVGs, no optimization needed
    <img
      src={`https://cdn.jsdelivr.net/gh/selfhst/icons/${formats[attempt]}/${icon}.${formats[attempt]}`}
      alt=""
      loading="lazy"
      onError={() => setAttempt(attempt + 1)}
      className={cn("object-contain", className)}
    />
  );
}
