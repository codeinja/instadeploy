"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronRight, CircleHelp } from "lucide-react";
import { navItems } from "@/components/app-sidebar";
import { CommandMenuTrigger } from "@/components/command-menu";
import { ThemeToggle } from "@/components/theme-toggle";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { buttonVariants } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

// Where you are, as a short breadcrumb: "Deployments › New".
function crumbs(pathname: string): { label: string; href?: string }[] {
  if (pathname === "/") return [{ label: "Overview" }];
  if (pathname === "/setup") return [{ label: "Setup guide" }];
  if (pathname === "/deployments/new")
    return [
      { label: "Deployments", href: "/deployments" },
      { label: "New deployment" },
    ];
  const section = navItems.find(
    (n) =>
      n.href !== "/" &&
      (pathname === n.href || pathname.startsWith(n.href + "/")),
  );
  if (!section) return [];
  if (pathname === section.href) return [{ label: section.label }];
  return [{ label: section.label, href: section.href }, { label: "Details" }];
}

export function AppHeader() {
  const pathname = usePathname();
  const trail = crumbs(pathname);
  return (
    <header className="sticky top-0 z-20 flex h-14 shrink-0 items-center gap-3 border-b bg-background/85 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/70 md:px-6">
      <SidebarTrigger className="-ml-1.5" />
      <nav
        aria-label="Breadcrumb"
        className="hidden min-w-0 items-center gap-1.5 text-sm sm:flex"
      >
        {trail.map((c, i) => (
          <span key={i} className="flex min-w-0 items-center gap-1.5">
            {i > 0 && (
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            {c.href ? (
              <Link
                href={c.href}
                className="truncate text-muted-foreground hover:text-foreground"
              >
                {c.label}
              </Link>
            ) : (
              <span className="truncate font-medium">{c.label}</span>
            )}
          </span>
        ))}
      </nav>
      <div className="ml-auto flex items-center gap-1.5">
        <CommandMenuTrigger className="w-40 md:w-64" />
        <Tooltip>
          <TooltipTrigger
            render={
              <Link
                href="/setup"
                className={buttonVariants({ variant: "ghost", size: "icon" })}
                aria-label="Setup guide"
              />
            }
          >
            <CircleHelp />
          </TooltipTrigger>
          <TooltipContent>Setup guide</TooltipContent>
        </Tooltip>
        <ThemeToggle />
      </div>
    </header>
  );
}
