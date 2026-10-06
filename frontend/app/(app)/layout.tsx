import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { auth } from "@/lib/auth";
import { AppSidebar } from "@/components/app-sidebar";
import { AppHeader } from "@/components/app-header";
import { CommandMenuProvider } from "@/components/command-menu";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";

// Every page in (app) requires a valid session. The check runs on the
// server, so signed-out users never see the dashboard shell.
export default async function AppLayout({ children }: LayoutProps<"/">) {
  const session = await auth.api.getSession({ headers: await headers() });
  if (!session) redirect("/login");

  return (
    <CommandMenuProvider>
      <SidebarProvider>
        <AppSidebar
          user={{ name: session.user.name, email: session.user.email }}
        />
        <SidebarInset className="bg-background">
          <AppHeader />
          <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 md:px-8 md:py-8">
            {children}
          </main>
        </SidebarInset>
      </SidebarProvider>
    </CommandMenuProvider>
  );
}
