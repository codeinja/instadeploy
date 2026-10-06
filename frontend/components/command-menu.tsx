"use client";

import { useRouter } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import { useTheme } from "next-themes";
import {
  Boxes,
  CircleHelp,
  Moon,
  Plus,
  Search,
  Server,
  Store,
  Sun,
  Terminal,
} from "lucide-react";
import { api } from "@/lib/api";
import type { CatalogApp, Deployment } from "@/lib/types";
import { navGroups } from "@/components/app-sidebar";
import { StatusBadge } from "@/components/status";
import { AppIcon } from "@/components/app-icon";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command";
import { cn } from "@/lib/utils";

const CommandMenuContext = createContext<() => void>(() => {});

// Opens the command menu from anywhere (e.g. the header search button).
export function useOpenCommandMenu() {
  return useContext(CommandMenuContext);
}

// ⌘K / Ctrl+K: jump to any page, deployment or app, or start an action.
export function CommandMenuProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const { resolvedTheme, setTheme } = useTheme();
  const [open, setOpen] = useState(false);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [apps, setApps] = useState<CatalogApp[]>([]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (!open) return;
    api<Deployment[]>("/deployments")
      .then(setDeployments)
      .catch(() => {});
    if (apps.length === 0)
      api<CatalogApp[]>("/apps")
        .then(setApps)
        .catch(() => {});
  }, [open, apps.length]);

  const go = useCallback(
    (href: string) => {
      setOpen(false);
      router.push(href);
    },
    [router],
  );

  return (
    <CommandMenuContext.Provider value={() => setOpen(true)}>
      {children}
      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search"
        description="Jump to a page, deployment or app"
      >
        <Command>
          <CommandInput placeholder="Search pages, deployments, apps…" />
          <CommandList className="max-h-[60vh]">
            <CommandEmpty>Nothing found.</CommandEmpty>
            <CommandGroup heading="Actions">
              <CommandItem onSelect={() => go("/deployments/new")}>
                <Plus /> New deployment
              </CommandItem>
              <CommandItem onSelect={() => go("/deployments/new?type=RUN")}>
                <Terminal /> Paste a docker run command
              </CommandItem>
              <CommandItem onSelect={() => go("/apps")}>
                <Store /> Browse the App Store
              </CommandItem>
              <CommandItem onSelect={() => go("/agents")}>
                <Server /> Connect a machine
              </CommandItem>
              <CommandItem onSelect={() => go("/setup")}>
                <CircleHelp /> Open the setup guide
              </CommandItem>
              <CommandItem
                onSelect={() => {
                  setTheme(resolvedTheme === "dark" ? "light" : "dark");
                  setOpen(false);
                }}
              >
                {resolvedTheme === "dark" ? <Sun /> : <Moon />} Switch to{" "}
                {resolvedTheme === "dark" ? "light" : "dark"} mode
              </CommandItem>
            </CommandGroup>
            {deployments.length > 0 && (
              <>
                <CommandSeparator />
                <CommandGroup heading="Deployments">
                  {deployments.map((d) => (
                    <CommandItem
                      key={d.id}
                      value={`deployment ${d.name} ${d.project_name}`}
                      onSelect={() => go(`/deployments/${d.id}`)}
                    >
                      <Boxes />
                      <span className="flex-1 truncate">{d.name}</span>
                      <StatusBadge status={d.status} />
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            )}
            <CommandSeparator />
            <CommandGroup heading="Go to">
              {navGroups
                .flatMap((g) => g.items)
                .map((item) => (
                  <CommandItem
                    key={item.href}
                    value={`page ${item.label}`}
                    onSelect={() => go(item.href)}
                  >
                    <item.icon /> {item.label}
                  </CommandItem>
                ))}
            </CommandGroup>
            {apps.length > 0 && (
              <>
                <CommandSeparator />
                <CommandGroup heading="App Store">
                  {apps.map((a) => (
                    <CommandItem
                      key={a.id}
                      value={`app ${a.name} ${a.category}`}
                      onSelect={() => go(`/apps/${a.id}`)}
                    >
                      <AppIcon icon={a.icon} name={a.name} className="size-4" />
                      <span className="flex-1 truncate">{a.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {a.category}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </CommandDialog>
    </CommandMenuContext.Provider>
  );
}

// The header's search field look-alike that opens the command menu.
export function CommandMenuTrigger({ className }: { className?: string }) {
  const open = useOpenCommandMenu();
  const [mac, setMac] = useState(false);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- platform is only known in the browser
    setMac(/Mac|iPhone|iPad/.test(navigator.platform));
  }, []);
  return (
    <button
      onClick={open}
      className={cn(
        "flex h-8 w-full items-center gap-2 rounded-lg border bg-card px-2.5 text-sm text-muted-foreground shadow-xs transition-colors hover:border-ring/40 hover:text-foreground",
        className,
      )}
    >
      <Search className="size-4" />
      <span className="flex-1 text-left">Search…</span>
      <CommandShortcut className="rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px] tracking-normal">
        {mac ? "⌘" : "Ctrl"} K
      </CommandShortcut>
    </button>
  );
}
