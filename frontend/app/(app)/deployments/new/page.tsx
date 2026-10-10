"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ArrowLeft,
  ArrowRight,
  Store,
  Terminal,
  Box,
  ChevronDown,
  CircleAlert,
  FolderGit2,
  Globe,
  HardDrive,
  KeyRound,
  Rocket,
  Server,
  SlidersHorizontal,
  FileCode2,
  FileUp,
  GitBranch,
  Layers,
  Loader2,
  Lock,
  Plus,
  Search,
  TriangleAlert,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type {
  Agent,
  ComposeAnalysis,
  ComposeDraft,
  Deployment,
  DeploymentType,
  DockerRunConversion,
  GitHubStatus,
  ImageInfo,
  Me,
  Project,
  SourceAnalysis,
  Spec,
} from "@/lib/types";
import { nameFromGit, nameFromImage } from "@/lib/format";
import { clearDraft, readDraft, saveDraft } from "@/lib/draft";
import { cn } from "@/lib/utils";
import { PageHeader } from "@/components/page-header";
import { EmptyState } from "@/components/empty-state";
import { ExposureBadge } from "@/components/exposure";
import {
  PublicAccessDialog,
  type AccessChoice,
} from "@/components/public-access-dialog";
import { SimpleSelect } from "@/components/simple-select";
import { EnvEditor, type EnvRow } from "@/components/env-editor";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const types: {
  type: DeploymentType;
  title: string;
  description: string;
  icon: typeof Box;
  example: string;
}[] = [
  {
    type: "IMAGE",
    title: "Docker Image",
    description: "Run an image from Docker Hub, GHCR or any registry.",
    icon: Box,
    example: "nginx:latest",
  },
  {
    type: "DOCKERFILE",
    title: "Dockerfile",
    description:
      "Build from a Git repository with a Dockerfile, public or private.",
    icon: FileCode2,
    example: "Dockerfile + source",
  },
  {
    type: "COMPOSE",
    title: "Docker Compose",
    description: "Run a multi-service app from a compose.yaml.",
    icon: Layers,
    example: "web + api + postgres",
  },
];

export default function NewDeploymentPage() {
  return (
    <Suspense>
      <NewDeployment />
    </Suspense>
  );
}

function NewDeployment() {
  const params = useSearchParams();
  const router = useRouter();
  const type = params.get("type") as DeploymentType | null;

  const project = params.get("project")
    ? `&project=${params.get("project")}`
    : "";
  if (type === ("RUN" as DeploymentType))
    return <DockerRunConvert project={project} />;
  if (!type || !types.some((t) => t.type === type)) {
    const options = [
      ...types,
      {
        type: "RUN",
        title: "docker run command",
        description: "Paste a docker run command, with all its options.",
        icon: Terminal,
        example: "docker run -p 80:80 nginx",
      },
    ];
    return (
      <div className="space-y-6">
        <PageHeader
          title="What do you want to deploy?"
          description="Pick a starting point. You'll get a public HTTPS URL either way."
        />
        <Link href="/apps" className="group block">
          <Card className="relative overflow-hidden border-primary/30 transition-all group-hover:border-primary/50 group-hover:shadow-md">
            <div className="pointer-events-none absolute -top-16 -right-16 size-48 rounded-full bg-primary/15 blur-3xl" />
            <CardContent className="relative flex items-center gap-4">
              <div className="bg-brand flex size-11 shrink-0 items-center justify-center rounded-xl text-primary-foreground shadow-sm">
                <Store className="size-5" />
              </div>
              <div className="flex-1">
                <p className="font-semibold">
                  App Store <Badge className="ml-1 align-middle">Easiest</Badge>
                </p>
                <p className="text-sm text-muted-foreground">
                  One-click apps: Paperless-ngx, Immich, Jellyfin, Nextcloud,
                  Vaultwarden and more.
                </p>
              </div>
              <ArrowRight className="size-5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
            </CardContent>
          </Card>
        </Link>
        <div className="flex items-center gap-3 text-xs font-medium tracking-wide text-muted-foreground uppercase">
          <span className="h-px flex-1 bg-border" /> or bring your own{" "}
          <span className="h-px flex-1 bg-border" />
        </div>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {options.map((t) => (
            <button
              key={t.type}
              onClick={() =>
                router.push(`/deployments/new?type=${t.type}${project}`)
              }
              className="group rounded-xl text-left focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <Card className="h-full transition-all group-hover:-translate-y-0.5 group-hover:border-primary/40 group-hover:shadow-md">
                <CardHeader>
                  <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground transition-colors group-hover:bg-primary group-hover:text-primary-foreground">
                    <t.icon className="size-5" />
                  </div>
                  <CardTitle>{t.title}</CardTitle>
                  <CardDescription>{t.description}</CardDescription>
                </CardHeader>
                <CardContent>
                  <code className="rounded-md bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
                    {t.example}
                  </code>
                </CardContent>
              </Card>
            </button>
          ))}
        </div>
      </div>
    );
  }
  return (
    <DeployForm
      type={type}
      initialProject={params.get("project") ?? ""}
      fromDraft={params.get("draft") === "1"}
    />
  );
}

function DockerRunConvert({ project }: { project: string }) {
  const router = useRouter();
  const [command, setCommand] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function convert(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await api<DockerRunConversion>("/convert/docker-run", {
        method: "POST",
        body: { command },
      });
      saveDraft({
        name: r.name,
        compose: r.compose,
        env: r.variables,
        publicService: r.service,
        port: r.ports.length === 1 ? r.ports[0] : undefined,
        warnings: r.warnings,
        source: "Converted from your docker run command",
      });
      router.push(`/deployments/new?type=COMPOSE&draft=1${project}`);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <form onSubmit={convert} className="mx-auto max-w-3xl space-y-6">
      <div className="space-y-2">
        <Link
          href="/deployments/new"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-4" /> Change type
        </Link>
        <PageHeader
          icon={<StepIcon icon={Terminal} />}
          title="Deploy a docker run command"
          description="Paste the command from an app's README. Options like -p, -e, -v, --restart and the command after the image are kept."
        />
      </div>
      <Card>
        <CardContent className="space-y-3">
          <Label htmlFor="cmd">docker run command</Label>
          <Textarea
            id="cmd"
            autoFocus
            required
            rows={8}
            className="bg-muted/30 font-mono text-xs"
            placeholder={
              "docker run -d \\\n  --name uptime-kuma \\\n  -p 3001:3001 \\\n  -v uptime-kuma:/app/data \\\n  louislam/uptime-kuma:1"
            }
            value={command}
            onChange={(e) => setCommand(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            Nothing is run here: the command is turned into a Compose file you
            can review before deploying. Environment variables become editable
            variables (passwords and tokens are marked secret).
          </p>
          {error && (
            <p className="rounded-lg border border-destructive/25 bg-destructive/5 p-3 text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-end">
            <Button type="submit" disabled={busy || !command.trim()}>
              {busy && <Loader2 className="animate-spin" />} Review Compose file{" "}
              <ArrowRight />
            </Button>
          </div>
        </CardContent>
      </Card>
    </form>
  );
}

interface ServiceRow {
  name: string;
  image: string;
  build?: string;
  ports: number[];
  public: boolean;
  port: string;
}

function DeployForm({
  type,
  initialProject,
  fromDraft,
}: {
  type: DeploymentType;
  initialProject: string;
  fromDraft: boolean;
}) {
  const router = useRouter();
  const meta = types.find((t) => t.type === type)!;

  // Where
  const [agents, setAgents] = useState<Agent[] | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [me, setMe] = useState<Me | null>(null);
  const [agentId, setAgentId] = useState("");
  const [projectId, setProjectId] = useState(initialProject);
  const [newProject, setNewProject] = useState("");
  const [environment, setEnvironment] = useState("production");
  const [draft] = useState<ComposeDraft | null>(() =>
    fromDraft && typeof window !== "undefined" ? readDraft() : null,
  );
  useEffect(clearDraft, []);
  const [name, setName] = useState(draft?.name ?? "");
  const nameTouched = useRef(!!name);

  // What
  const [image, setImage] = useState("");
  const [tag, setTag] = useState("latest");
  const [imageInfo, setImageInfo] = useState<ImageInfo | null>(null);
  const [inspecting, setInspecting] = useState(false);
  const [inspectError, setInspectError] = useState<string | null>(null);

  const [sourceKind, setSourceKind] = useState<"git" | "inline">(
    type === "COMPOSE" ? "inline" : "git",
  );
  const [github, setGitHub] = useState<GitHubStatus | null>(null);
  const [gitUrl, setGitUrl] = useState("");
  const [gitBranch, setGitBranch] = useState("main");
  const [gitAnalysis, setGitAnalysis] = useState<SourceAnalysis | null>(null);
  const [analyzingGit, setAnalyzingGit] = useState(false);
  const [autoDeploy, setAutoDeploy] = useState(true);
  const [path, setPath] = useState("");
  const [composeText, setComposeText] = useState(draft?.compose ?? "");
  const [composeAnalysis, setComposeAnalysis] =
    useState<ComposeAnalysis | null>(null);
  const [composeError, setComposeError] = useState<string | null>(null);

  // Single-container settings
  const [port, setPort] = useState("");
  // Private by default: going public always goes through the dialog.
  const [isPublic, setIsPublic] = useState(false);
  const [access, setAccess] = useState<Record<string, AccessChoice>>({});
  const [askPublic, setAskPublic] = useState<{
    service: string;
    editing: boolean;
  } | null>(null);
  const [services, setServices] = useState<ServiceRow[]>([]);
  const [env, setEnv] = useState<EnvRow[]>(draft?.env ?? []);
  const [volumes, setVolumes] = useState<{ source: string; target: string }[]>(
    [],
  );
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [restartPolicy, setRestartPolicy] = useState("unless-stopped");
  const [cpus, setCpus] = useState("");
  const [memory, setMemory] = useState("");
  const [healthPath, setHealthPath] = useState("");
  const [healthInterval, setHealthInterval] = useState("30");

  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      api<Agent[]>("/agents"),
      api<Project[]>("/projects"),
      api<Me>("/me"),
    ])
      .then(([a, p, m]) => {
        setAgents(a);
        setProjects(p);
        setMe(m);
        const preferred =
          a.find((x) => x.status === "ONLINE") ??
          a.find((x) => x.status !== "REVOKED");
        if (preferred) setAgentId(preferred.id);
        if (!initialProject)
          setProjectId(
            p.find((x) => x.name === "Default")?.id ?? p[0]?.id ?? "new",
          );
      })
      .catch((e) => setFormError(errorMessage(e)));
    // Repositories the user's GitHub App can read, for the picker. Optional:
    // public repositories work without it.
    if (type !== "IMAGE")
      api<GitHubStatus>("/github")
        .then(setGitHub)
        .catch(() => {});
  }, [initialProject, type]);

  const suggestName = (n: string) => {
    if (!nameTouched.current && n) setName(n);
  };

  // ---- Docker image: detect ports from the registry.
  async function inspectImage() {
    const ref = image.trim();
    if (!ref) return;
    const full = ref.includes("@") || !tag ? ref : `${ref}:${tag}`;
    setInspecting(true);
    setInspectError(null);
    try {
      const info = await api<ImageInfo>("/analyze/image", {
        method: "POST",
        body: { image: full, agent_id: agentId },
      });
      setImageInfo(info);
      if (info.exposed_ports.length === 1 && !port)
        setPort(String(info.exposed_ports[0]));
    } catch (e) {
      setImageInfo(null);
      setInspectError(errorMessage(e));
    } finally {
      setInspecting(false);
    }
  }

  async function analyzeGit(url = gitUrl, branch = gitBranch) {
    if (!agentId) return toast.error("Choose a machine first");
    setAnalyzingGit(true);
    setGitAnalysis(null);
    try {
      const res = await api<SourceAnalysis>("/analyze/git", {
        method: "POST",
        body: {
          git_url: url.trim(),
          git_branch: branch.trim(),
          agent_id: agentId,
        },
      });
      setGitAnalysis(res);
      applyAnalysis(res);
      suggestName(nameFromGit(url));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAnalyzingGit(false);
    }
  }

  // Fill the form from what was found in the project.
  function applyAnalysis(a: SourceAnalysis) {
    if (type === "DOCKERFILE") {
      const df = a.dockerfiles[0];
      setPath(df?.path ?? "Dockerfile");
      if (df?.ports.length === 1) setPort(String(df.ports[0]));
    } else {
      setPath(a.compose_path ?? "");
      setComposeError(
        a.compose_error ??
          (a.compose_files.length === 0
            ? "No compose.yaml found in the project."
            : null),
      );
      if (a.compose) setComposeFromAnalysis(a.compose);
    }
  }

  const setComposeFromAnalysis = useCallback(
    (c: ComposeAnalysis) => {
      setComposeAnalysis(c);
      setServices((prev) =>
        c.services.map((s) => {
          const old = prev.find((p) => p.name === s.name);
          if (!old && draft?.publicService === s.name) {
            const port = draft.port
              ? String(draft.port)
              : s.ports.length === 1
                ? String(s.ports[0])
                : "";
            return {
              name: s.name,
              image: s.image,
              build: s.build,
              ports: s.ports,
              // Suggested, but still private until the user turns it on.
              public: false,
              port,
            };
          }
          // Never make services public automatically.
          return {
            name: s.name,
            image: s.image,
            build: s.build,
            ports: s.ports,
            public: old?.public ?? false,
            port: old?.port ?? (s.ports.length === 1 ? String(s.ports[0]) : ""),
          };
        }),
      );
    },
    [draft],
  );

  // Compose YAML pasted or opened from a file: analyze as the user types.
  useEffect(() => {
    if (type !== "COMPOSE" || sourceKind !== "inline") return;
    if (!composeText.trim()) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- clear analysis when the editor is emptied
      setComposeAnalysis(null);
      setServices([]);
      return;
    }
    const t = setTimeout(async () => {
      try {
        const res = await api<ComposeAnalysis>("/analyze/compose", {
          method: "POST",
          body: { content: composeText },
        });
        setComposeError(null);
        setComposeFromAnalysis(res);
      } catch (e) {
        setComposeError(errorMessage(e));
      }
    }, 500);
    return () => clearTimeout(t);
  }, [composeText, sourceKind, type, setComposeFromAnalysis]);

  const dockerfiles = useMemo(
    () => gitAnalysis?.dockerfiles ?? [],
    [gitAnalysis],
  );
  const detectedPorts = useMemo(() => {
    if (type === "IMAGE") return imageInfo?.exposed_ports ?? [];
    return dockerfiles.find((d) => d.path === path)?.ports ?? [];
  }, [type, imageInfo, dockerfiles, path]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);

    let spec: Spec;
    const healthCheck = healthPath
      ? { path: healthPath, interval_seconds: Number(healthInterval) || 30 }
      : undefined;
    if (type === "COMPOSE") {
      const missingPort = services.find((s) => s.public && !s.port);
      if (missingPort)
        return setFormError(
          `Choose which port of ${missingPort.name} to make public.`,
        );
      spec = {
        type,
        source:
          sourceKind === "inline"
            ? { kind: "inline", compose: composeText }
            : {
                kind: "git",
                git_url: gitUrl.trim(),
                git_branch: gitBranch.trim(),
                path,
              },
        services: services.map((s) => ({
          name: s.name,
          public: s.public,
          port: s.port ? Number(s.port) : undefined,
          auth_hint:
            draft?.publicService === s.name ? draft.authHint : undefined,
        })),
      };
      if (services.length === 0)
        return setFormError("Add a Compose file with at least one service.");
    } else {
      if (isPublic && !port)
        return setFormError("Choose which port to make public.");
      spec = {
        type,
        services: [
          {
            name: "web",
            port: port ? Number(port) : undefined,
            public: isPublic,
            health_check: healthCheck,
          },
        ],
        volumes: volumes.filter((v) => v.source && v.target),
        restart_policy: restartPolicy,
        cpus: cpus ? Number(cpus) : undefined,
        memory_mb: memory ? Number(memory) : undefined,
      };
      if (type === "IMAGE") {
        const ref = image.trim();
        spec.image =
          ref.includes("@") || !tag.trim() ? ref : `${ref}:${tag.trim()}`;
      } else {
        spec.source = {
          kind: "git",
          git_url: gitUrl.trim(),
          git_branch: gitBranch.trim(),
          path: path || "Dockerfile",
        };
        if (!gitUrl.trim())
          return setFormError("Enter the Git repository to build from.");
      }
    }

    setSubmitting(true);
    try {
      let pid = projectId;
      if (projectId === "new") {
        const p = await api<Project>("/projects", {
          method: "POST",
          body: { name: newProject.trim() || "My project" },
        });
        pid = p.id;
      }
      const d = await api<Deployment>("/deployments", {
        method: "POST",
        body: {
          project_id: pid,
          agent_id: agentId,
          name,
          environment,
          auto_deploy: sourceKind === "git" && type !== "IMAGE" && autoDeploy,
          spec,
          access: Object.entries(access)
            .filter(
              ([svc, a]) =>
                a.mode !== "none" &&
                (type === "COMPOSE"
                  ? services.some((s) => s.name === svc && s.public)
                  : isPublic),
            )
            .map(([svc, a]) => ({ service: svc, ...a })),
          variables: env
            .filter((v) => v.key.trim())
            .map((v) => ({
              key: v.key.trim(),
              value: v.value,
              secret: v.secret,
              service: v.service,
            })),
        },
      });
      toast.success(`Deploying ${d.name}…`);
      router.push(`/deployments/${d.id}?tab=logs`);
    } catch (e) {
      setFormError(errorMessage(e));
      setSubmitting(false);
    }
  }

  const agent = agents?.find((a) => a.id === agentId);

  if (agents && agents.length === 0) {
    return (
      <div className="space-y-6">
        <PageHeader title={`Deploy a ${meta.title}`} />
        <EmptyState
          icon={Server}
          title="Connect a machine first"
          description="Insta Deploy runs containers on your own machines through a small agent."
          action={
            <Link href="/agents" className={buttonVariants({ size: "sm" })}>
              Connect a machine
            </Link>
          }
        />
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="mx-auto max-w-3xl space-y-6">
      <div className="space-y-2">
        <Link
          href="/deployments/new"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-4" /> Change type
        </Link>
        <PageHeader
          icon={<StepIcon icon={meta.icon} />}
          title={
            draft?.source ? `Deploy ${draft.name}` : `Deploy a ${meta.title}`
          }
          description={draft?.source ?? meta.description}
        />
      </div>
      {draft?.warnings && draft.warnings.length > 0 && (
        <div className="space-y-1.5 rounded-xl border border-warning/40 bg-warning/10 p-4 text-sm">
          <p className="font-medium">
            A few things were changed or need your attention:
          </p>
          {draft.warnings.map((w) => (
            <p key={w} className="flex gap-2">
              <TriangleAlert className="mt-0.5 size-4 shrink-0 text-[oklch(0.55_0.13_70)] dark:text-warning" />{" "}
              {w}
            </p>
          ))}
        </div>
      )}

      {/* ------------------------------------------------ source */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <SectionIcon icon={type === "IMAGE" ? Box : FolderGit2} />{" "}
            {type === "IMAGE" ? "Image" : "Source"}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {type === "IMAGE" && (
            <>
              <div className="grid gap-3 sm:grid-cols-[1fr_10rem]">
                <div className="space-y-2">
                  <Label htmlFor="image">Docker image</Label>
                  <Input
                    id="image"
                    required
                    autoFocus
                    className="font-mono"
                    placeholder="nginx, postgres, ghcr.io/user/app…"
                    value={image}
                    onChange={(e) => {
                      let v = e.target.value.trim();
                      // Pasted "nginx:1.27"? Split the tag out.
                      const slash = v.lastIndexOf("/");
                      const colon = v.lastIndexOf(":");
                      if (colon > slash && !v.includes("@")) {
                        setTag(v.slice(colon + 1));
                        v = v.slice(0, colon);
                      }
                      setImage(v);
                      setImageInfo(null);
                      suggestName(nameFromImage(v));
                    }}
                    onBlur={inspectImage}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="tag">Tag</Label>
                  <Input
                    id="tag"
                    className="font-mono"
                    placeholder="latest"
                    value={tag}
                    onChange={(e) => setTag(e.target.value.trim())}
                    onBlur={inspectImage}
                  />
                </div>
              </div>
              <PortDetection
                loading={inspecting}
                error={inspectError}
                ports={imageInfo ? imageInfo.exposed_ports : null}
                source="the image"
                onCheck={inspectImage}
                canCheck={!!image}
              />
            </>
          )}

          {type === "COMPOSE" && (
            <Tabs
              value={sourceKind}
              onValueChange={(v) => setSourceKind(v as typeof sourceKind)}
            >
              <TabsList>
                <TabsTrigger value="inline">compose.yaml</TabsTrigger>
                <TabsTrigger value="git">
                  <GitBranch /> Git repository
                </TabsTrigger>
              </TabsList>
            </Tabs>
          )}

          {type === "COMPOSE" && sourceKind === "inline" && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label htmlFor="compose">Compose file</Label>
                <label
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                >
                  <FileUp /> Open file…
                  <input
                    type="file"
                    accept=".yml,.yaml"
                    className="hidden"
                    onChange={async (e) => {
                      const f = e.target.files?.[0];
                      if (f) setComposeText(await f.text());
                    }}
                  />
                </label>
              </div>
              <Textarea
                id="compose"
                rows={12}
                className="min-h-64 bg-muted/30 font-mono text-xs"
                placeholder={
                  'services:\n  web:\n    image: nginx:latest\n    ports:\n      - "80:80"\n  redis:\n    image: redis:latest'
                }
                value={composeText}
                onChange={(e) => setComposeText(e.target.value)}
              />
            </div>
          )}

          {type !== "IMAGE" && sourceKind === "git" && (
            <div className="space-y-3">
              {github?.connected && github.repositories.length > 0 && (
                <div className="space-y-2">
                  <Label htmlFor="repo">Your GitHub repositories</Label>
                  <SimpleSelect
                    id="repo"
                    placeholder="Choose a repository…"
                    value={
                      github.repositories.some((r) => r.clone_url === gitUrl)
                        ? gitUrl
                        : ""
                    }
                    onChange={(url) => {
                      const repo = github.repositories.find(
                        (r) => r.clone_url === url,
                      );
                      const branch = repo?.default_branch || "main";
                      setGitUrl(url);
                      setGitBranch(branch);
                      analyzeGit(url, branch);
                    }}
                    options={github.repositories.map((r) => ({
                      value: r.clone_url,
                      label: r.private
                        ? `${r.full_name} (private)`
                        : r.full_name,
                    }))}
                  />
                </div>
              )}
              <div className="grid gap-3 sm:grid-cols-[1fr_10rem_auto] sm:items-end">
                <div className="space-y-2">
                  <Label htmlFor="git">Repository URL</Label>
                  <Input
                    id="git"
                    className="font-mono"
                    placeholder="https://github.com/user/my-app"
                    value={gitUrl}
                    onChange={(e) => {
                      setGitUrl(e.target.value);
                      setGitAnalysis(null);
                    }}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="branch">Branch</Label>
                  <Input
                    id="branch"
                    className="font-mono"
                    value={gitBranch}
                    onChange={(e) => {
                      setGitBranch(e.target.value);
                      setGitAnalysis(null);
                    }}
                  />
                </div>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => analyzeGit()}
                  disabled={!gitUrl || analyzingGit}
                >
                  {analyzingGit ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <Search />
                  )}{" "}
                  Analyze
                </Button>
              </div>
              <PrivateRepoHint github={github} />
              {gitAnalysis && (
                <p className="text-xs text-muted-foreground">
                  Found {gitAnalysis.dockerfiles.length} Dockerfile(s) and{" "}
                  {gitAnalysis.compose_files.length} Compose file(s) at commit{" "}
                  <code>{gitAnalysis.git_commit?.slice(0, 7)}</code>.
                </p>
              )}
              <label className="flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/40">
                <Switch
                  checked={autoDeploy}
                  onCheckedChange={setAutoDeploy}
                  className="mt-0.5"
                />
                <span>
                  <span className="block text-sm font-medium">Auto deploy</span>
                  <span className="block text-xs text-muted-foreground">
                    Build and redeploy when {gitBranch || "the branch"} gets new
                    commits: checked every minute
                    {github?.webhook_secret_set
                      ? ", and right away on push through your GitHub App"
                      : ""}
                    .
                  </span>
                </span>
              </label>
            </div>
          )}

          {type === "DOCKERFILE" && gitAnalysis && (
            <div className="space-y-2">
              <Label>Dockerfile</Label>
              {dockerfiles.length === 0 ? (
                <p className="text-sm text-destructive">
                  No Dockerfile found in the project.
                </p>
              ) : (
                <SimpleSelect
                  value={path}
                  onChange={setPath}
                  options={dockerfiles.map((d) => ({
                    value: d.path,
                    label: d.path,
                  }))}
                />
              )}
              <PortDetection
                ports={detectedPorts}
                source="the Dockerfile (EXPOSE)"
              />
            </div>
          )}

          {type === "COMPOSE" &&
            sourceKind === "git" &&
            gitAnalysis &&
            gitAnalysis.compose_files.length > 0 && (
              <div className="space-y-2">
                <Label>Compose file</Label>
                <SimpleSelect
                  value={path}
                  onChange={setPath}
                  options={gitAnalysis.compose_files.map((f) => ({
                    value: f,
                    label: f,
                  }))}
                />
              </div>
            )}
          {type === "COMPOSE" && composeError && (
            <p className="text-sm text-destructive">{composeError}</p>
          )}
        </CardContent>
      </Card>

      {/* ------------------------------------------------ services */}
      {type === "COMPOSE" ? (
        services.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <SectionIcon icon={Layers} /> Services
              </CardTitle>
              <CardDescription>
                Choose which services get a public URL. Everything else stays
                private to the project.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="overflow-x-auto rounded-lg border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Service</TableHead>
                      <TableHead>Detected ports</TableHead>
                      <TableHead className="w-24">Public</TableHead>
                      <TableHead className="w-36">Port</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {services.map((s, i) => (
                      <TableRow
                        key={s.name}
                        className={cn(s.public && "bg-accent/30")}
                      >
                        <TableCell>
                          <div className="font-medium">{s.name}</div>
                          <div className="font-mono text-xs text-muted-foreground">
                            {s.image || (s.build ? `build: ${s.build}` : "")}
                          </div>
                        </TableCell>
                        <TableCell className="text-sm">
                          {s.ports.length ? (
                            s.ports.join(", ")
                          ) : (
                            <span className="text-muted-foreground">
                              none found
                            </span>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col items-start gap-1">
                            <Switch
                              checked={s.public}
                              onCheckedChange={(v) =>
                                v
                                  ? setAskPublic({
                                      service: s.name,
                                      editing: false,
                                    })
                                  : setServices(
                                      services.map((x, j) =>
                                        j === i ? { ...x, public: false } : x,
                                      ),
                                    )
                              }
                              aria-label={`Make ${s.name} public`}
                            />
                            {s.public ? (
                              <AccessSummary
                                choice={access[s.name]}
                                onChange={() =>
                                  setAskPublic({
                                    service: s.name,
                                    editing: true,
                                  })
                                }
                              />
                            ) : (
                              draft?.publicService === s.name && (
                                <span className="text-[11px] text-muted-foreground">
                                  Suggested
                                </span>
                              )
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          {s.public && (
                            <Input
                              type="number"
                              min={1}
                              max={65535}
                              placeholder={
                                s.ports.length > 1 ? "Choose" : "Port"
                              }
                              list={`ports-${s.name}`}
                              value={s.port}
                              onChange={(e) =>
                                setServices(
                                  services.map((x, j) =>
                                    j === i
                                      ? { ...x, port: e.target.value }
                                      : x,
                                  ),
                                )
                              }
                            />
                          )}
                          <datalist id={`ports-${s.name}`}>
                            {s.ports.map((p) => (
                              <option key={p} value={p} />
                            ))}
                          </datalist>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              {me && services.some((s) => s.public) && (
                <p className="text-xs text-muted-foreground">
                  Public services get URLs like{" "}
                  <code>
                    https://{name || "app"}
                    {services.filter((s) => s.public).length > 1
                      ? `-${services.find((s) => s.public)?.name}`
                      : ""}
                    -xxxx.{me.apps_domain}
                  </code>
                </p>
              )}
              {composeAnalysis?.warnings.length ? (
                <div className="space-y-1.5 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
                  {composeAnalysis.warnings.map((w) => (
                    <p key={w} className="flex gap-2">
                      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-[oklch(0.55_0.13_70)] dark:text-warning" />{" "}
                      {w}
                    </p>
                  ))}
                </div>
              ) : null}
            </CardContent>
          </Card>
        )
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <SectionIcon icon={Globe} /> Network
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="port">Container port</Label>
                <Input
                  id="port"
                  type="number"
                  min={1}
                  max={65535}
                  placeholder={
                    detectedPorts.length > 1
                      ? `Choose: ${detectedPorts.join(", ")}`
                      : "80"
                  }
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                  list="detected-ports"
                />
                <datalist id="detected-ports">
                  {detectedPorts.map((p) => (
                    <option key={p} value={p} />
                  ))}
                </datalist>
                <p className="text-xs text-muted-foreground">
                  The port your app listens on inside the container.
                </p>
              </div>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors sm:mt-6",
                  isPublic
                    ? "border-primary/30 bg-accent/40"
                    : "hover:bg-muted/40",
                )}
              >
                <Switch
                  checked={isPublic}
                  onCheckedChange={(v) =>
                    v
                      ? setAskPublic({ service: "web", editing: false })
                      : setIsPublic(false)
                  }
                  className="mt-0.5"
                />
                <span className="space-y-1">
                  <span className="block text-sm font-medium">Public</span>
                  {isPublic && (
                    <AccessSummary
                      choice={access.web}
                      onChange={() =>
                        setAskPublic({ service: "web", editing: true })
                      }
                    />
                  )}
                  <span className="block text-xs text-muted-foreground">
                    {!isPublic &&
                      "Private by default: it runs without a URL until you turn this on. "}
                    {me ? (
                      <>
                        Get a URL like{" "}
                        <code>
                          https://{name || "my-app"}-xxxx.{me.apps_domain}
                        </code>
                      </>
                    ) : (
                      "Get a public HTTPS URL"
                    )}
                  </span>
                </span>
              </label>
            </div>
          </CardContent>
        </Card>
      )}

      {/* ------------------------------------------------ variables */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <SectionIcon icon={KeyRound} /> Environment variables
          </CardTitle>
          <CardDescription>
            Mark sensitive values as secrets: they&apos;re encrypted, never
            shown again, and redacted from logs. Project-wide variables can be
            set on the project.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <EnvEditor
            rows={env}
            onChange={setEnv}
            services={
              type === "COMPOSE" ? services.map((s) => s.name) : undefined
            }
          />
        </CardContent>
      </Card>

      {/* ------------------------------------------------ volumes & advanced */}
      {type !== "COMPOSE" && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <SectionIcon icon={HardDrive} /> Volumes
            </CardTitle>
            <CardDescription>
              Keep data across redeploys, e.g. a volume named postgres-data at
              /var/lib/postgresql/data.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {volumes.map((v, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input
                  placeholder="volume name"
                  className="font-mono"
                  value={v.source}
                  onChange={(e) =>
                    setVolumes(
                      volumes.map((x, j) =>
                        j === i ? { ...x, source: e.target.value } : x,
                      ),
                    )
                  }
                />
                <span className="text-muted-foreground">→</span>
                <Input
                  placeholder="/path/in/container"
                  className="font-mono"
                  value={v.target}
                  onChange={(e) =>
                    setVolumes(
                      volumes.map((x, j) =>
                        j === i ? { ...x, target: e.target.value } : x,
                      ),
                    )
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setVolumes(volumes.filter((_, j) => j !== i))}
                  aria-label="Remove volume"
                >
                  <X />
                </Button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() =>
                setVolumes([...volumes, { source: "", target: "" }])
              }
            >
              <Plus /> Add volume
            </Button>
          </CardContent>
        </Card>
      )}

      {type !== "COMPOSE" && (
        <Card>
          <button
            type="button"
            aria-expanded={showAdvanced}
            className="flex w-full items-center justify-between gap-3 px-6 text-left"
            onClick={() => setShowAdvanced(!showAdvanced)}
          >
            <span className="flex items-center gap-2">
              <SectionIcon icon={SlidersHorizontal} />
              <span>
                <span className="block font-medium">Advanced</span>
                <span className="block text-sm text-muted-foreground">
                  Restart policy, resource limits and health check
                </span>
              </span>
            </span>
            <ChevronDown
              className={cn(
                "size-4 transition-transform",
                showAdvanced && "rotate-180",
              )}
            />
          </button>
          {showAdvanced && (
            <CardContent className="grid gap-4 border-t pt-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>Restart policy</Label>
                <SimpleSelect
                  value={restartPolicy}
                  onChange={setRestartPolicy}
                  options={[
                    {
                      value: "unless-stopped",
                      label: "Unless stopped (recommended)",
                    },
                    { value: "always", label: "Always" },
                    { value: "on-failure", label: "On failure" },
                    { value: "no", label: "Never" },
                  ]}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <Label htmlFor="cpus">CPU limit</Label>
                  <Input
                    id="cpus"
                    type="number"
                    min={0}
                    step={0.25}
                    placeholder="e.g. 1"
                    value={cpus}
                    onChange={(e) => setCpus(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="memory">Memory (MB)</Label>
                  <Input
                    id="memory"
                    type="number"
                    min={6}
                    step={64}
                    placeholder="e.g. 512"
                    value={memory}
                    onChange={(e) => setMemory(e.target.value)}
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="health">HTTP health check path</Label>
                <Input
                  id="health"
                  className="font-mono"
                  placeholder="/health (optional)"
                  value={healthPath}
                  onChange={(e) => setHealthPath(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Only needed if the image has no Docker HEALTHCHECK.
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="interval">Check every (seconds)</Label>
                <Input
                  id="interval"
                  type="number"
                  min={5}
                  value={healthInterval}
                  onChange={(e) => setHealthInterval(e.target.value)}
                  disabled={!healthPath}
                />
              </div>
            </CardContent>
          )}
        </Card>
      )}

      {/* ------------------------------------------------ where */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <SectionIcon icon={Rocket} /> Where it runs
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="name">Name</Label>
            <Input
              id="name"
              required
              placeholder="my-app"
              pattern="[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?"
              title="Lowercase letters, numbers and dashes"
              value={name}
              onChange={(e) => {
                nameTouched.current = true;
                setName(e.target.value.toLowerCase());
              }}
            />
          </div>
          <div className="space-y-2">
            <Label>Deploy to</Label>
            <SimpleSelect
              value={agentId}
              onChange={setAgentId}
              placeholder="Choose a machine"
              options={(agents ?? []).map((a) => ({
                value: a.id,
                label: `${a.name} (${a.status.toLowerCase()})`,
                disabled: a.status === "REVOKED",
              }))}
            />
            {agent?.status === "OFFLINE" && (
              <p className="text-xs text-[oklch(0.55_0.13_70)] dark:text-warning">
                This machine is currently offline. The deployment will start
                when it reconnects.
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label>Project</Label>
            <SimpleSelect
              value={projectId}
              onChange={setProjectId}
              options={[
                ...projects.map((p) => ({ value: p.id, label: p.name })),
                { value: "new", label: "+ New project…" },
              ]}
            />
            {projectId === "new" && (
              <Input
                placeholder="Project name"
                value={newProject}
                onChange={(e) => setNewProject(e.target.value)}
                autoFocus
              />
            )}
          </div>
          <div className="space-y-2">
            <Label>Environment</Label>
            <SimpleSelect
              value={environment}
              onChange={setEnvironment}
              options={[
                { value: "production", label: "Production" },
                { value: "staging", label: "Staging" },
                { value: "development", label: "Development" },
              ]}
            />
          </div>
        </CardContent>
      </Card>

      {formError && (
        <p className="flex items-start gap-2 rounded-lg border border-destructive/25 bg-destructive/5 p-3 text-sm whitespace-pre-wrap text-destructive">
          <CircleAlert className="mt-0.5 size-4 shrink-0" /> {formError}
        </p>
      )}
      {askPublic && (
        <PublicAccessDialog
          open
          onOpenChange={(o) => !o && setAskPublic(null)}
          serviceName={
            type === "COMPOSE" ? askPublic.service : name || "this app"
          }
          authHint={
            draft?.publicService === askPublic.service
              ? draft.authHint
              : undefined
          }
          authNote={
            draft?.publicService === askPublic.service
              ? draft.authNote
              : undefined
          }
          editing={askPublic.editing}
          currentAccess={access[askPublic.service]?.mode ?? "none"}
          onConfirm={(choice) => {
            const prev = access[askPublic.service];
            setAccess({
              ...access,
              // Editing without a new secret keeps the one typed before.
              [askPublic.service]:
                choice.mode !== "none" && !choice.secret && prev
                  ? prev
                  : choice,
            });
            if (type === "COMPOSE")
              setServices(
                services.map((x) =>
                  x.name === askPublic.service ? { ...x, public: true } : x,
                ),
              );
            else setIsPublic(true);
          }}
        />
      )}
      <div className="sticky bottom-4 z-20 rounded-xl border bg-background/90 px-4 py-3 shadow-lg backdrop-blur">
        <div className="flex items-center justify-between gap-3">
          <p className="hidden min-w-0 truncate text-sm text-muted-foreground sm:block">
            {name ? (
              <span className="font-medium text-foreground">{name}</span>
            ) : (
              "Your app"
            )}
            {agent ? ` on ${agent.name}` : ""}
            {environment !== "production" ? ` · ${environment}` : ""}
          </p>
          <div className="ml-auto flex gap-2">
            <Link
              href="/deployments"
              className={buttonVariants({ variant: "ghost" })}
            >
              Cancel
            </Link>
            <Button type="submit" size="lg" disabled={submitting || !agentId}>
              {submitting ? <Loader2 className="animate-spin" /> : <Rocket />}{" "}
              Deploy
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}

// Shows which ports were detected, without silently picking one when
// there's a choice.
function PortDetection({
  ports,
  source,
  loading,
  error,
  onCheck,
  canCheck,
}: {
  ports: number[] | null;
  source: string;
  loading?: boolean;
  error?: string | null;
  onCheck?: () => void;
  canCheck?: boolean;
}) {
  if (loading)
    return (
      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3 animate-spin" /> Checking {source}…
      </p>
    );
  if (error)
    return (
      <p className="text-xs text-[oklch(0.55_0.13_70)] dark:text-warning">
        {error} You can still enter the port yourself.
      </p>
    );
  if (ports === null)
    return onCheck && canCheck ? (
      <button
        type="button"
        className="text-xs text-muted-foreground underline underline-offset-4"
        onClick={onCheck}
      >
        Detect ports
      </button>
    ) : null;
  if (ports.length === 0)
    return (
      <p className="text-xs text-muted-foreground">
        No ports declared by {source}. Enter the port your app listens on.
      </p>
    );
  return (
    <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
      {ports.length === 1
        ? "Detected port"
        : "Detected ports (choose one below)"}{" "}
      from {source}:
      {ports.map((p) => (
        <Badge key={p} variant="secondary" className="font-mono">
          {p}
        </Badge>
      ))}
    </p>
  );
}

function StepIcon({ icon: Icon }: { icon: typeof Box }) {
  return (
    <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-accent text-accent-foreground">
      <Icon className="size-5" />
    </span>
  );
}

function SectionIcon({ icon: Icon }: { icon: typeof Box }) {
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
      <Icon className="size-4" />
    </span>
  );
}

// Explains how private repositories work, depending on whether the user
// has connected a GitHub App.
function PrivateRepoHint({ github }: { github: GitHubStatus | null }) {
  if (!github?.connected) {
    return (
      <p className="flex gap-1.5 text-xs text-muted-foreground">
        <Lock className="mt-px size-3.5 shrink-0" />
        <span>
          Public repositories work as is. For a private GitHub repository,{" "}
          <Link
            href="/settings#github"
            className="font-medium text-primary hover:underline"
          >
            connect a GitHub App in Settings
          </Link>{" "}
          (step-by-step guide included), then install it on the repository.
        </span>
      </p>
    );
  }
  const manage = github.installations[0]?.html_url ?? github.install_url;
  return (
    <p className="flex gap-1.5 text-xs text-muted-foreground">
      <Lock className="mt-px size-3.5 shrink-0" />
      <span>
        Private repositories are read through your GitHub App{" "}
        <b>{github.name}</b>.{" "}
        {github.error ? (
          <span className="text-destructive">{github.error}</span>
        ) : (
          <>
            Repository missing?{" "}
            <a
              href={manage}
              target="_blank"
              rel="noreferrer"
              className="font-medium text-primary hover:underline"
            >
              Give the app access to it on GitHub
            </a>
            , then reload this page.
          </>
        )}
      </span>
    </p>
  );
}

// The protection chosen for a public service, with a link to change it.
function AccessSummary({
  choice,
  onChange,
}: {
  choice?: AccessChoice;
  onChange: () => void;
}) {
  const mode = choice?.mode ?? "none";
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <ExposureBadge exposure={mode === "none" ? "public" : "protected"} />
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          onChange();
        }}
        className="text-[11px] text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
      >
        {mode === "none" ? "Add protection" : "Change"}
      </button>
    </span>
  );
}
