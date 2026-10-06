import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { auth } from "@/lib/auth";
import { AuthForm } from "@/components/auth-form";

export default async function Page() {
  const session = await auth.api.getSession({ headers: await headers() });
  if (session) redirect("/");
  return <AuthForm mode="login" />;
}
