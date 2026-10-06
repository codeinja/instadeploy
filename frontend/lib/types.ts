// Types mirror the Go backend's JSON (backend/*.go).

export type AgentStatus = "ONLINE" | "OFFLINE" | "REVOKED";

export type DeploymentType = "IMAGE" | "DOCKERFILE" | "COMPOSE";

export type DeploymentStatus = "QUEUED" | "BUILDING" | "DEPLOYING" | "RUNNING" | "STOPPED" | "FAILED" | "DELETING";

export type RouteStatus = "PENDING" | "CREATING" | "READY" | "FAILED" | "DISABLED";

export type Health = "HEALTHY" | "UNHEALTHY" | "STARTING" | "NONE";

export type Environment = "development" | "staging" | "production";

export interface Agent {
  id: string;
  name: string;
  status: AgentStatus;
  last_seen: string | null;
  hostname: string;
  os: string;
  arch: string;
  docker_version: string;
  agent_version: string;
  deployments: number;
  tunnel_ready: boolean;
  tunnel_error: string;
  created_at: string;
}

export interface HealthCheck {
  path: string;
  interval_seconds: number;
}

export interface ServiceSpec {
  name: string;
  port?: number;
  public: boolean;
  health_check?: HealthCheck;
}

export interface VolumeSpec {
  source: string;
  target: string;
  read_only?: boolean;
}

export interface Source {
  kind: "upload" | "git" | "inline";
  upload_id?: string;
  git_url?: string;
  git_branch?: string;
  path?: string;
  compose?: string;
}

export interface Spec {
  type: DeploymentType;
  image?: string;
  source?: Source;
  services: ServiceSpec[];
  volumes?: VolumeSpec[];
  restart_policy?: string;
  cpus?: number;
  memory_mb?: number;
}

export interface Stats {
  cpu_percent: number;
  memory_bytes: number;
  memory_limit: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  at: string;
}

export interface Route {
  id: string;
  kind: "GENERATED" | "CUSTOM";
  domain_id: string | null;
  hostname: string;
  url: string;
  status: RouteStatus;
  error: string;
}

export interface Service {
  id: string;
  name: string;
  image: string;
  port: number | null;
  public: boolean;
  target_host: string;
  container_id: string;
  state: string;
  health: Health;
  detected_ports: number[];
  routes: Route[];
  stats: Stats | null;
}

export interface Revision {
  id: string;
  number: number;
  spec: Spec;
  image_digest: string;
  git_commit: string;
  trigger: "deploy" | "redeploy" | "rollback" | "auto" | "config";
  rollback_of: number | null;
  status: string;
  error: string;
  created_at: string;
  finished_at: string | null;
}

export interface Deployment {
  id: string;
  project_id: string;
  project_name: string;
  agent_id: string;
  agent_name: string;
  agent_status: AgentStatus;
  name: string;
  type: DeploymentType;
  environment: Environment;
  spec: Spec;
  status: DeploymentStatus;
  error: string;
  auto_deploy: boolean;
  revision: Revision | null;
  services: Service[];
  pending_action: string;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  deployments: number;
  running: number;
  failed: number;
  created_at: string;
}

export interface Variable {
  id: string;
  project_id: string;
  deployment_id: string | null;
  service: string | null;
  environment: Environment | null;
  key: string;
  value: string;
  secret: boolean;
  updated_at: string;
}

export interface DNSRecord {
  type: string;
  name: string;
  value: string;
}

export interface Domain {
  id: string;
  hostname: string;
  project_id: string | null;
  service_id: string | null;
  target: string | null;
  deployment_id: string | null;
  status: "PENDING" | "ACTIVE" | "FAILED";
  error: string;
  dns_records: DNSRecord[];
  ssl_status: "UNKNOWN" | "PENDING" | "VALID" | "INVALID";
  route_status: RouteStatus | null;
  route_error: string | null;
  created_at: string;
}

export interface ActivityEvent {
  id: number;
  level: "info" | "success" | "error";
  message: string;
  project_id: string | null;
  deployment_id: string | null;
  agent_id: string | null;
  created_at: string;
}

export interface RegistryCredential {
  id: string;
  server: string;
  username: string;
  created_at: string;
}

export interface LogLine {
  id: number;
  revision_id: string | null;
  stream: "build" | "deploy";
  line: string;
  created_at: string;
}

export interface Me {
  id: string;
  name: string;
  email: string;
  is_admin: boolean;
  setup_complete: boolean;
  apps_domain: string;
  api_url: string;
  pangolin_enabled: boolean;
}

// Analysis results used by the deploy form.

export interface ImageInfo {
  image: string;
  digest: string;
  exposed_ports: number[];
}

export interface ComposeService {
  name: string;
  image: string;
  build?: string;
  ports: number[];
  environment: string[];
  volumes: string[];
  depends_on: string[];
  healthcheck: boolean;
}

export interface ComposeAnalysis {
  services: ComposeService[];
  warnings: string[];
}

export interface SourceAnalysis {
  root: string;
  files: number;
  dockerfiles: { path: string; ports: number[] }[];
  compose_files: string[];
  compose?: ComposeAnalysis;
  compose_path?: string;
  compose_error?: string;
  git_commit?: string;
}

export interface UploadResult {
  id: string;
  filename: string;
  size: number;
  analysis: SourceAnalysis;
}

export interface ServerSettings {
  public_api_url: string;
  agent_image: string;
  apps_domain: string;
  pangolin_enabled: boolean;
  pangolin_api_url: string;
  pangolin_org_id: string;
  pangolin_endpoint: string;
  pangolin_key_set: boolean;
  setup_complete: boolean;
  is_admin: boolean;
}

export interface PangolinDomain {
  id: string;
  domain: string;
  type: string;
  verified: boolean;
}

export interface CatalogInput {
  key: string;
  label: string;
  description?: string;
  default?: string;
  generate?: "password" | "secret";
  secret: boolean;
  hidden: boolean;
}

export interface CatalogApp {
  id: string;
  name: string;
  category: string;
  icon: string;
  website: string;
  tagline: string;
  description: string;
  notes?: string;
  public: { service: string; port: number };
  inputs: CatalogInput[];
  compose: string;
}

export interface DockerRunConversion {
  name: string;
  service: string;
  compose: string;
  variables: { key: string; value: string; secret: boolean }[];
  ports: number[];
  warnings: string[];
}

// A prefilled Compose deployment, handed from the App Store or the
// docker run converter to the deploy form.
export interface ComposeDraft {
  name: string;
  compose: string;
  env: { key: string; value: string; secret: boolean }[];
  publicService?: string;
  port?: number;
  warnings?: string[];
  source?: string;
}
