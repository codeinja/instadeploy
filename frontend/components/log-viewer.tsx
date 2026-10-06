"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

// A dark, monospace log panel that sticks to the bottom while new lines
// arrive (unless the user scrolled up to read).
export function LogViewer({
  lines,
  empty = "No output yet.",
  className,
}: {
  lines: {
    key: string | number;
    text: string;
    tone?: "muted" | "error" | "success";
  }[];
  empty?: string;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [stick, setStick] = useState(true);

  useEffect(() => {
    if (stick && ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [lines, stick]);

  return (
    <div
      ref={ref}
      onScroll={(e) => {
        const el = e.currentTarget;
        setStick(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
      }}
      className={cn(
        "h-[28rem] overflow-auto rounded-xl border border-zinc-800 bg-zinc-950 shadow-inner p-4 font-mono text-xs leading-relaxed text-zinc-100",
        className,
      )}
    >
      {lines.length === 0 ? (
        <p className="text-zinc-500">{empty}</p>
      ) : (
        lines.map((l) => (
          <div
            key={l.key}
            className={cn(
              "break-all whitespace-pre-wrap",
              l.tone === "muted" && "text-zinc-500",
              l.tone === "error" && "text-red-400",
              l.tone === "success" && "text-emerald-400",
            )}
          >
            {l.text || " "}
          </div>
        ))
      )}
    </div>
  );
}
