import type { Deployment, Service } from "./types";

export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "never";
  const seconds = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 5) return "just now";
  if (seconds < 60) return `${seconds} seconds ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return minutes === 1 ? "1 minute ago" : `${minutes} minutes ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return hours === 1 ? "1 hour ago" : `${hours} hours ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return days === 1 ? "yesterday" : `${days} days ago`;
  return new Date(iso).toLocaleDateString();
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export const typeLabels: Record<Deployment["type"], string> = {
  IMAGE: "Docker image",
  DOCKERFILE: "Dockerfile",
  COMPOSE: "Docker Compose",
};

// What a deployment is built from, in a few words.
export function deploymentSource(d: Pick<Deployment, "type" | "spec">): string {
  const src = d.spec.source;
  if (d.type === "IMAGE") return d.spec.image ?? "";
  if (!src) return "";
  if (src.kind === "git") return `${src.git_url?.replace(/^https:\/\//, "")} (${src.git_branch})`;
  if (src.kind === "inline") return "compose.yaml";
  return src.path ? `upload · ${src.path}` : "uploaded project";
}

// The URLs people care about: ready routes, custom domains first.
export function publicUrls(d: Deployment): { service: string; url: string; kind: string }[] {
  return d.services.flatMap((s) =>
    s.routes.filter((r) => r.status === "READY").map((r) => ({ service: s.name, url: r.url, kind: r.kind })),
  );
}

export function primaryUrl(d: Deployment): string | null {
  const urls = publicUrls(d);
  return (urls.find((u) => u.kind === "CUSTOM") ?? urls[0])?.url ?? null;
}

export function routeProblem(s: Service): string | null {
  const failed = s.routes.find((r) => r.status === "FAILED");
  return failed ? failed.error : null;
}

export function greeting(): string {
  const h = new Date().getHours();
  if (h < 5) return "Good evening";
  if (h < 12) return "Good morning";
  if (h < 18) return "Good afternoon";
  return "Good evening";
}

// "ghcr.io/acme/my-app:1.2" -> "my-app"
export function nameFromImage(image: string): string {
  const last = image.split("/").pop() ?? "";
  return slug(last.split(/[:@]/)[0]);
}

export function nameFromGit(url: string): string {
  return slug((url.split("/").pop() ?? "").replace(/\.git$/, ""));
}

export function slug(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32)
    .replace(/-+$/, "");
}
