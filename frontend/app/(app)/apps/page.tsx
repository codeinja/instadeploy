"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ArrowRight, Search, Terminal } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import type { CatalogApp } from "@/lib/types";
import { cn } from "@/lib/utils";
import { PageHeader } from "@/components/page-header";
import { AppIcon } from "@/components/app-icon";
import { ErrorState } from "@/components/error-state";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";

export default function AppStorePage() {
  const [apps, setApps] = useState<CatalogApp[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("All");

  useEffect(() => {
    api<CatalogApp[]>("/apps")
      .then(setApps)
      .catch((e) => setError(errorMessage(e)));
  }, []);

  const categories = useMemo(
    () => [
      "All",
      ...Array.from(new Set((apps ?? []).map((a) => a.category))).sort(),
    ],
    [apps],
  );
  const visible = (apps ?? []).filter((a) => {
    const needle = q.trim().toLowerCase();
    const matches =
      !needle ||
      [a.name, a.tagline, a.category, a.description]
        .join(" ")
        .toLowerCase()
        .includes(needle);
    return matches && (category === "All" || a.category === category);
  });

  return (
    <div className="space-y-6">
      <PageHeader
        title="App Store"
        description="Popular self-hosted apps, ready to deploy on your machines with a public URL."
        actions={
          <Link
            href="/deployments/new?type=RUN"
            className={buttonVariants({ variant: "outline" })}
          >
            <Terminal /> Paste a docker run command
          </Link>
        }
      />
      <div className="flex flex-col-reverse gap-3">
        <div className="flex flex-wrap gap-1.5">
          {categories.map((c) => (
            <button
              key={c}
              onClick={() => setCategory(c)}
              aria-pressed={category === c}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm font-medium transition-colors",
                category === c
                  ? "border-primary bg-primary text-primary-foreground shadow-xs"
                  : "bg-card text-muted-foreground hover:border-ring/40 hover:text-foreground",
              )}
            >
              {c}
              <span
                className={cn(
                  "text-xs tabular-nums",
                  category === c
                    ? "text-primary-foreground/70"
                    : "text-muted-foreground/70",
                )}
              >
                {c === "All"
                  ? (apps ?? []).length
                  : (apps ?? []).filter((a) => a.category === c).length}
              </span>
            </button>
          ))}
        </div>
        <div className="relative sm:max-w-sm">
          <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search apps…"
            className="bg-card pl-8"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            aria-label="Search apps"
          />
        </div>
      </div>

      {error && <ErrorState message={error} />}
      {!apps && !error ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-32 rounded-xl" />
          ))}
        </div>
      ) : visible.length === 0 ? (
        <div className="rounded-xl border border-dashed py-12 text-center text-sm text-muted-foreground">
          No apps match. You can deploy any Docker image from{" "}
          <Link
            href="/deployments/new"
            className="font-medium text-foreground underline underline-offset-4"
          >
            New deployment
          </Link>
          .
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {visible.map((a) => (
            <Link
              key={a.id}
              href={`/apps/${a.id}`}
              className="group rounded-xl focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <Card className="h-full transition-all group-hover:-translate-y-0.5 group-hover:border-primary/40 group-hover:shadow-md">
                <CardContent className="flex h-full flex-col gap-4">
                  <div className="flex items-start justify-between gap-3">
                    <span className="flex size-12 shrink-0 items-center justify-center rounded-xl border bg-background p-2 shadow-xs">
                      <AppIcon
                        icon={a.icon}
                        name={a.name}
                        className="size-full"
                      />
                    </span>
                    <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                      {a.category}
                    </span>
                  </div>
                  <div className="min-w-0 flex-1 space-y-1">
                    <p className="truncate font-semibold">{a.name}</p>
                    <p className="line-clamp-2 text-sm text-muted-foreground">
                      {a.tagline}
                    </p>
                  </div>
                  <span className="inline-flex items-center gap-1 text-sm font-medium text-primary opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                    Deploy <ArrowRight className="size-3.5" />
                  </span>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
