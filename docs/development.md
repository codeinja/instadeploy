# Development

How to run, change and check each part of Insta Deploy locally.

## Requirements

- Docker with Compose v2.24+
- Go 1.26+ (API and agent)
- Node.js 24+ (dashboard)

## Run everything in Docker

The simplest way to develop is the same stack you'd install:

```bash
docker compose up -d --build                 # postgres, migrate, backend, frontend, agent image
docker compose up -d --build frontend        # rebuild one part
docker compose build agent-image             # rebuild the agent image
```

After rebuilding the agent image, restart the agent container (`insta-deploy-agent`) on the machine so it uses the new image. The **Machines → Issue new token** dialog gives you a fresh `docker run` command.

| Service | Port | Notes |
| --- | --- | --- |
| frontend | 3000 | Dashboard |
| backend | 8080 | API; reference at `/docs` |
| postgres | 5432 | Bound to `127.0.0.1` only (default credentials `insta`/`insta`) |

## Run the parts directly

Start the database and apply migrations:

```bash
docker compose up -d postgres
export DATABASE_URL="postgres://insta:insta@localhost:5432/insta?sslmode=disable"
(cd backend && go run . migrate)
```

**API** on port 8080:

```bash
cd backend && go run .
```

**Dashboard** on port 3000. It needs the database because Better Auth stores accounts there:

```bash
cd frontend
npm install
BACKEND_URL=http://localhost:8080 npm run dev
```

**Agent**, against your local Docker:

```bash
cd agent
INSTA_DEPLOY_TOKEN=idt_... \
INSTA_DEPLOY_SERVER=http://localhost:8080 \
INSTA_DEPLOY_ALLOW_HTTP=true \
INSTA_DEPLOY_DATA_DIR=$HOME/.insta-deploy \
go run .
```

Get a token from **Machines → Add machine**, and set the setup guide's server address to `http://localhost:8080`. Outside a container, the agent needs the `docker` CLI (with Compose and Buildx) and `git` on your `PATH`.

## Agent settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `INSTA_DEPLOY_TOKEN` | required | The machine's token |
| `INSTA_DEPLOY_SERVER` | required | API URL; must be `https://` unless `INSTA_DEPLOY_ALLOW_HTTP=true` |
| `INSTA_DEPLOY_ALLOW_HTTP` | `false` | Allow `http://` (local networks only) |
| `INSTA_DEPLOY_ALLOWED_HOST_PATHS` | none | Host folders Compose deployments may mount, comma-separated |
| `INSTA_DEPLOY_DATA_DIR` | `/var/lib/insta-deploy` | Project files and build contexts |
| `NEWT_IMAGE` | `fosrl/newt:latest` | Pangolin tunnel client image |

To run agents on other machines, push the agent image to a registry and set `AGENT_IMAGE` in `.env`, so the connect command uses it.

## Checks

Run these before committing:

```bash
(cd backend && gofmt -l . && go vet ./...)
(cd agent && gofmt -l . && go vet ./...)
(cd frontend && npx tsc --noEmit && npm run lint && npm run build)
```

## Code map

### API (`backend/`, a single Go package)

| File | Contents |
| --- | --- |
| `main.go` | Routes, server startup, `migrate` command |
| `config.go`, `settings.go`, `handlers_settings.go` | Environment config and dashboard-edited settings |
| `auth.go` | Session lookup (Better Auth tables) and agent tokens |
| `spec.go`, `validate.go` | Deployment spec and input validation |
| `deployments.go`, `handlers_deployments.go` | Deployments, services, routes, revisions |
| `tasks.go`, `handlers_agentapi.go` | Agent task queue, payloads, variable resolution, log redaction |
| `routes.go` | Route reconciler and custom-domain verification |
| `pangolin.go` | `PangolinService`, the only Pangolin-specific code |
| `apps.go`, `catalog.yaml` | App Store |
| `dockerrun.go` | `docker run` → Compose converter |
| `analyze.go`, `registry.go`, `git.go` | Compose analysis, registry inspection, Git branches and auto-deploy polling |
| `github.go` | GitHub App: installation tokens for private repositories, repository list, push webhooks |
| `crypto.go` | AES-256-GCM encryption |
| `migrations/` | SQL migrations, applied in order |
| `openapi.yaml` | API reference served at `/docs` |

To change the schema, add `migrations/00N_<name>.sql`. Never edit an applied migration.

### Agent (`agent/`)

| File | Contents |
| --- | --- |
| `main.go` | Startup, registration, command loop, heartbeat, tunnel (`newt`) |
| `deploy.go` | Task handlers: deploy (all types), lifecycle, delete, logs, analyze |
| `docker.go` | `DockerService`, an Engine API client |
| `cli.go` | Fixed-argument runs of `docker build`, `docker compose` and `git` |
| `compose.go` | Compose precheck, validation and transformation |
| `files.go` | Path checks and copying project files |
| `health.go`, `logs.go` | HTTP health checks and log shipping |

Rules for agent code:

- Run `docker` and `git` with argument lists only. Never build a shell command string.
- Anything that lets a deployment reach the host (mounts, capabilities, devices, network modes) must be checked in `precheckComposeFiles` or `validateCompose`, with a clear error message.

### Dashboard (`frontend/`)

| Path | Contents |
| --- | --- |
| `lib/auth.ts`, `lib/auth-client.ts` | Better Auth server and client |
| `lib/api.ts` | API client (`/api` proxy) |
| `proxy.ts` | Redirects signed-out visitors to `/login` |
| `app/(auth)/` | Sign-in and registration |
| `app/(app)/` | Signed-in pages; the layout checks the session on the server |
| `app/api/[...path]/route.ts` | Proxy to the Go API |
| `components/ui/` | shadcn/ui components (`npx shadcn@latest add <name>`) |
| `components/deployment/` | Deployment page tabs |
| `components/status.tsx` | Status badges, progress stepper, URL chips |

The shadcn components are built on Base UI: compose them with the `render` prop (`<DropdownMenuTrigger render={<Button />}>`), not `asChild`.

## API

The API reference is served at `http://localhost:8080/docs`. [examples/deploy-nginx.sh](examples/deploy-nginx.sh) signs in and deploys nginx through the API, exactly as the dashboard does. [examples/nginx.json](examples/nginx.json) and [examples/compose-stack.json](examples/compose-stack.json) are sample request bodies.
