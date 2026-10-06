"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  Activity,
  Boxes,
  ChevronsUpDown,
  FolderKanban,
  Globe,
  KeyRound,
  LayoutDashboard,
  LogOut,
  Plus,
  Rocket,
  Server,
  Settings,
  Store,
} from "lucide-react";
import { signOut } from "@/lib/auth-client";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";

export const navGroups = [
  {
    label: "Deploy",
    items: [
      { href: "/", label: "Overview", icon: LayoutDashboard },
      { href: "/deployments", label: "Deployments", icon: Boxes },
      { href: "/apps", label: "App Store", icon: Store },
    ],
  },
  {
    label: "Manage",
    items: [
      { href: "/projects", label: "Projects", icon: FolderKanban },
      { href: "/agents", label: "Machines", icon: Server },
      { href: "/domains", label: "Domains", icon: Globe },
      { href: "/variables", label: "Variables & Secrets", icon: KeyRound },
    ],
  },
  {
    label: "Account",
    items: [
      { href: "/activity", label: "Activity", icon: Activity },
      { href: "/settings", label: "Settings", icon: Settings },
    ],
  },
];

// Kept for places that want the flat list.
export const navItems = navGroups.flatMap((g) => g.items);

export function isActive(pathname: string, href: string) {
  if (href === "/") return pathname === "/";
  if (href === "/deployments")
    return (
      pathname === href ||
      (pathname.startsWith("/deployments/") && pathname !== "/deployments/new")
    );
  return pathname === href || pathname.startsWith(href + "/");
}

export function BrandMark({ className = "size-8" }: { className?: string }) {
  return (
    <div
      className={`bg-brand flex shrink-0 items-center justify-center rounded-lg text-white shadow-sm shadow-primary/30 ${className}`}
    >
      <Rocket className="size-4" />
    </div>
  );
}

export function AppSidebar({
  user,
}: {
  user: { name: string; email: string };
}) {
  const pathname = usePathname();
  const router = useRouter();
  const initials = (user.name || user.email)
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();

  async function logout() {
    await signOut();
    router.push("/login");
    router.refresh();
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="gap-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={<Link href="/" />}
              className="hover:bg-transparent"
            >
              <BrandMark />
              <div className="grid flex-1 text-left leading-tight">
                <span className="font-semibold tracking-tight">
                  Insta Deploy
                </span>
                <span className="text-xs text-muted-foreground">
                  Deploy Docker. Get a URL.
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem className="mt-3 group-data-[collapsible=icon]:mt-2">
            <SidebarMenuButton
              tooltip="New deployment"
              render={<Link href="/deployments/new" />}
              isActive={pathname === "/deployments/new"}
              className="justify-center bg-primary font-medium text-primary-foreground shadow-sm hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground data-active:bg-primary data-active:text-primary-foreground group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:rounded-lg group-data-[collapsible=icon]:p-0! group-data-[collapsible=icon]:border group-data-[collapsible=icon]:border-dashed group-data-[collapsible=icon]:border-primary/50 group-data-[collapsible=icon]:bg-transparent group-data-[collapsible=icon]:text-primary group-data-[collapsible=icon]:shadow-none group-data-[collapsible=icon]:hover:border-solid group-data-[collapsible=icon]:hover:bg-primary group-data-[collapsible=icon]:hover:text-primary-foreground"
            >
              <Plus />
              <span className="group-data-[collapsible=icon]:hidden">New deployment</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {navGroups.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      isActive={isActive(pathname, item.href)}
                      tooltip={item.label}
                      render={<Link href={item.href} />}
                    >
                      <item.icon />
                      <span>{item.label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger render={<SidebarMenuButton size="lg" />}>
                <Avatar className="size-8 rounded-lg">
                  <AvatarFallback className="rounded-lg bg-accent text-xs font-semibold text-accent-foreground">
                    {initials}
                  </AvatarFallback>
                </Avatar>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-medium">{user.name}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {user.email}
                  </span>
                </div>
                <ChevronsUpDown className="ml-auto size-4 text-muted-foreground" />
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-56">
                <DropdownMenuLabel className="font-normal">
                  <div className="text-sm font-medium">{user.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {user.email}
                  </div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => router.push("/settings")}>
                  <Settings /> Account settings
                </DropdownMenuItem>
                <DropdownMenuItem onClick={logout}>
                  <LogOut /> Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
