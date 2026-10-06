-- Insta Deploy 0.2 data model:
--
--   project ─┬─ deployment ─┬─ revision (history, rollback)
--            │              ├─ service ── route ── (Pangolin)
--            │              └─ variable (deployment / service level)
--            ├─ variable (project level, optionally per environment)
--            └─ domain ── route
--
-- Existing deployments become single-service IMAGE deployments in a
-- "Default" project, keeping their container names and Pangolin routes.

CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

INSERT INTO projects (user_id, name)
SELECT DISTINCT user_id, 'Default' FROM agents;

-- Agents: machine details, and revocation (a revoked agent has no token).
ALTER TABLE agents
    ADD COLUMN hostname       text NOT NULL DEFAULT '',
    ADD COLUMN os             text NOT NULL DEFAULT '',
    ADD COLUMN arch           text NOT NULL DEFAULT '',
    ADD COLUMN docker_version text NOT NULL DEFAULT '',
    ADD COLUMN agent_version  text NOT NULL DEFAULT '',
    ADD COLUMN revoked_at     timestamptz,
    ALTER COLUMN token_hash DROP NOT NULL;

-- Deployments are rebuilt with the same IDs (container names derive from them).
ALTER TABLE deployments RENAME TO deployments_v1;
ALTER INDEX deployments_agent_id_idx RENAME TO deployments_v1_agent_id_idx;

CREATE TABLE deployments (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    agent_id            uuid NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    name                text NOT NULL,
    -- IMAGE, DOCKERFILE or COMPOSE
    type                text NOT NULL,
    -- development, staging or production
    environment         text NOT NULL DEFAULT 'production',
    -- The desired configuration (see spec.go). Never contains secret values.
    spec                jsonb NOT NULL,
    -- QUEUED, BUILDING, DEPLOYING, RUNNING, STOPPED, FAILED, DELETING
    status              text NOT NULL DEFAULT 'QUEUED',
    error               text NOT NULL DEFAULT '',
    auto_deploy         boolean NOT NULL DEFAULT false,
    current_revision_id uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deployments_agent_id_idx ON deployments (agent_id);
CREATE INDEX deployments_project_id_idx ON deployments (project_id);

CREATE TABLE deployment_revisions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id  uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    number         integer NOT NULL,
    spec           jsonb NOT NULL,
    -- What exactly ran, so a rollback can reproduce it.
    image_digest   text NOT NULL DEFAULT '',
    git_commit     text NOT NULL DEFAULT '',
    -- deploy, redeploy, rollback, auto (git change), config
    trigger        text NOT NULL DEFAULT 'deploy',
    rollback_of    uuid,
    -- QUEUED, BUILDING, DEPLOYING, RUNNING, STOPPED, FAILED
    status         text NOT NULL DEFAULT 'QUEUED',
    error          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    UNIQUE (deployment_id, number)
);

ALTER TABLE deployments
    ADD CONSTRAINT deployments_current_revision_fkey
    FOREIGN KEY (current_revision_id) REFERENCES deployment_revisions (id) ON DELETE SET NULL;

CREATE TABLE services (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    name          text NOT NULL,
    image         text NOT NULL DEFAULT '',
    -- The container port that public traffic is sent to (NULL: none chosen).
    port          integer,
    public        boolean NOT NULL DEFAULT false,
    -- Hostname the Pangolin tunnel uses to reach the container.
    target_host   text NOT NULL DEFAULT '',
    container_id  text NOT NULL DEFAULT '',
    -- Docker container state (running, exited, ...) and health
    -- (HEALTHY, UNHEALTHY, STARTING, NONE), as last reported by the agent.
    state         text NOT NULL DEFAULT '',
    health        text NOT NULL DEFAULT 'NONE',
    detected_ports integer[] NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (deployment_id, name)
);

CREATE TABLE domains (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    project_id   uuid REFERENCES projects (id) ON DELETE SET NULL,
    hostname     text NOT NULL UNIQUE,
    service_id   uuid REFERENCES services (id) ON DELETE SET NULL,
    -- PENDING (waiting for DNS), ACTIVE, FAILED
    status       text NOT NULL DEFAULT 'PENDING',
    error        text NOT NULL DEFAULT '',
    -- DNS records the user must create, as returned by the tunnel provider.
    dns_records  jsonb NOT NULL DEFAULT '[]',
    -- UNKNOWN, PENDING, VALID, INVALID
    ssl_status   text NOT NULL DEFAULT 'UNKNOWN',
    provider_ref text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- A public URL. GENERATED routes get <name>-xxxx.<APPS_DOMAIN>; CUSTOM
-- routes use a domain from the domains table.
CREATE TABLE routes (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id    uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    kind          text NOT NULL DEFAULT 'GENERATED',
    domain_id     uuid REFERENCES domains (id) ON DELETE CASCADE,
    hostname      text NOT NULL UNIQUE,
    url           text NOT NULL DEFAULT '',
    -- PENDING, CREATING, READY, FAILED, DISABLED
    status        text NOT NULL DEFAULT 'PENDING',
    error         text NOT NULL DEFAULT '',
    -- The provider's ID for this route (a Pangolin resource ID).
    provider_ref  text NOT NULL DEFAULT '',
    -- Where the route currently points, to detect when it must be updated.
    target_host   text NOT NULL DEFAULT '',
    target_port   integer NOT NULL DEFAULT 0,
    site_ref      text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX routes_deployment_id_idx ON routes (deployment_id);

-- Environment variables and secrets. Values are encrypted at rest
-- (AES-256-GCM, see crypto.go). Scope, from widest to narrowest:
--   project (environment NULL = all environments) < deployment < service
CREATE TABLE variables (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    deployment_id   uuid REFERENCES deployments (id) ON DELETE CASCADE,
    service_name    text,
    environment     text,
    key             text NOT NULL,
    value_encrypted bytea NOT NULL,
    is_secret       boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (project_id, deployment_id, service_name, environment, key)
);

CREATE TABLE registry_credentials (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    server             text NOT NULL,
    username           text NOT NULL,
    password_encrypted bytea NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, server)
);

-- Uploaded build contexts (ZIP). The file lives in DATA_DIR/uploads/<id>.zip.
CREATE TABLE uploads (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    filename   text NOT NULL,
    size       bigint NOT NULL,
    sha256     text NOT NULL,
    analysis   jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Work for agents. Payloads only hold references; secrets are resolved
-- when the task is handed to the agent and never stored here.
CREATE TABLE agent_tasks (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id      uuid NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    deployment_id uuid REFERENCES deployments (id) ON DELETE CASCADE,
    revision_id   uuid REFERENCES deployment_revisions (id) ON DELETE CASCADE,
    -- DEPLOY, STOP, START, RESTART, DELETE, LOGS, ANALYZE
    type          text NOT NULL,
    payload       jsonb NOT NULL DEFAULT '{}',
    -- PENDING, DISPATCHED, DONE, FAILED, CANCELLED
    status        text NOT NULL DEFAULT 'PENDING',
    result        jsonb,
    error         text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    dispatched_at timestamptz,
    finished_at   timestamptz
);
CREATE INDEX agent_tasks_pending_idx ON agent_tasks (agent_id, created_at) WHERE status IN ('PENDING', 'DISPATCHED');

CREATE TABLE deployment_logs (
    id            bigserial PRIMARY KEY,
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    revision_id   uuid REFERENCES deployment_revisions (id) ON DELETE CASCADE,
    -- build (docker build output) or deploy (what Insta Deploy is doing)
    stream        text NOT NULL,
    line          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deployment_logs_deployment_idx ON deployment_logs (deployment_id, id);

-- Activity feed ("Agent connected", "nginx deployed", ...).
CREATE TABLE events (
    id            bigserial PRIMARY KEY,
    user_id       text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    project_id    uuid,
    deployment_id uuid,
    agent_id      uuid,
    -- info, success, error
    level         text NOT NULL DEFAULT 'info',
    message       text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_user_idx ON events (user_id, id DESC);

-- Move existing deployments over.
INSERT INTO deployments (id, project_id, agent_id, name, type, spec, status, error, created_at, updated_at)
SELECT d.id, p.id, d.agent_id, d.name, 'IMAGE',
       jsonb_build_object(
           'type', 'IMAGE',
           'image', d.image,
           'restart_policy', 'unless-stopped',
           'services', jsonb_build_array(jsonb_build_object('name', 'web', 'port', d.container_port, 'public', true))),
       CASE d.status
           WHEN 'PENDING' THEN 'QUEUED'
           WHEN 'PULLING' THEN 'DEPLOYING'
           WHEN 'STARTING' THEN 'DEPLOYING'
           WHEN 'STOPPING' THEN 'STOPPED'
           ELSE d.status
       END,
       d.error, d.created_at, d.updated_at
FROM deployments_v1 d
JOIN agents a ON a.id = d.agent_id
JOIN projects p ON p.user_id = a.user_id AND p.name = 'Default';

INSERT INTO deployment_revisions (deployment_id, number, spec, status, error, created_at, finished_at)
SELECT id, 1, spec, CASE WHEN status IN ('RUNNING', 'STOPPED', 'FAILED') THEN status ELSE 'QUEUED' END,
       error, created_at, updated_at
FROM deployments;

UPDATE deployments d SET current_revision_id = r.id
FROM deployment_revisions r WHERE r.deployment_id = d.id;

INSERT INTO services (deployment_id, name, image, port, public, target_host, container_id, state)
SELECT d.id, 'web', d.image, d.container_port, true,
       'insta-' || d.name || '-' || substr(replace(d.id::text, '-', ''), 1, 8),
       d.container_id, CASE WHEN d.status = 'RUNNING' THEN 'running' ELSE '' END
FROM deployments_v1 d
WHERE EXISTS (SELECT 1 FROM deployments n WHERE n.id = d.id);

INSERT INTO routes (service_id, deployment_id, hostname, url, status, error, provider_ref, target_host, target_port, site_ref)
SELECT s.id, d.id,
       regexp_replace(d.public_url, '^https?://', ''),
       d.public_url,
       CASE d.url_status WHEN 'READY' THEN 'READY' WHEN 'FAILED' THEN 'FAILED' WHEN 'DISABLED' THEN 'DISABLED' ELSE 'PENDING' END,
       d.url_error,
       COALESCE(d.pangolin_resource_id::text, ''),
       s.target_host, s.port, COALESCE(a.pangolin_site_id::text, '')
FROM deployments_v1 d
JOIN services s ON s.deployment_id = d.id
JOIN agents a ON a.id = d.agent_id
WHERE d.public_url <> '';

-- Environment variables need encrypting with the server's key, which SQL
-- doesn't have. `backend migrate` moves these into "variables" right after
-- the SQL migrations run, then drops this table.
CREATE TABLE legacy_env (
    deployment_id uuid NOT NULL,
    project_id    uuid NOT NULL,
    key           text NOT NULL,
    value         text NOT NULL
);
INSERT INTO legacy_env (deployment_id, project_id, key, value)
SELECT d.id, n.project_id, e.key, e.value
FROM deployments_v1 d
JOIN deployments n ON n.id = d.id,
LATERAL jsonb_each_text(d.env) e;

DROP TABLE deployments_v1;
