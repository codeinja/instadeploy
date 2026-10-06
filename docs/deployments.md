# Deployments

Everything you can deploy, and the options for running and managing it.

Start from **New deployment** in the dashboard (or press `Ctrl/⌘ K`), then pick:

| Option | Use it for |
| --- | --- |
| [App Store](#app-store) | Popular self-hosted apps, one click |
| [docker run command](#docker-run-commands) | A command copied from an app's README |
| [Docker image](#docker-image) | Any image from Docker Hub, GHCR or another registry |
| [Dockerfile](#dockerfile) | Your own source code, uploaded as a ZIP or from Git |
| [Docker Compose](#docker-compose) | Multi-service apps |

For every option you also choose a **machine**, a **project** and an **environment** (production, staging or development). Names use lowercase letters, numbers and dashes.

## App Store

36 ready-made Compose templates (Immich, Jellyfin, Paperless-ngx, Nextcloud, Vaultwarden, n8n, Uptime Kuma, Gitea, Grafana and more), defined in [backend/catalog.yaml](../backend/catalog.yaml).

- Pick an app, review its settings, choose a machine, and click **Deploy**.
- Passwords and keys are generated in your browser and saved as **secret** variables. Copy any you need, such as an admin password, before deploying: secrets can't be shown again.
- **Customize Compose file** opens the template in the Compose form so you can edit it first.
- Apps that need to know their own address get it automatically through the variables below.

| Variable | Value |
| --- | --- |
| `INSTA_PUBLIC_URL` | `https://<app>-xxxx.<apps domain>`, or the custom domain once connected |
| `INSTA_PUBLIC_HOST` | The same, without `https://` |

These two variables are available to every deployment, not just App Store apps.

## docker run commands

Paste a `docker run` command; multi-line commands with `\` are fine. It's converted into a one-service Compose file that you review before deploying. Nothing is executed during conversion.

| In `docker run` | Becomes |
| --- | --- |
| `-e KEY=value` | A variable (marked secret if the name looks like a password, token or key) |
| `-p 8080:80` | Public port `80`; no port is opened on the machine |
| `-v name:/path`, `--mount` | A named volume; host folders must be allowed on the agent |
| `--restart`, `-w`, `-u`, `--entrypoint`, `--cpus`, `-m`, `--shm-size`, `--tmpfs`, `--add-host`, `--init`, `--read-only` | The matching Compose settings |
| Arguments after the image | `command:` |
| `--privileged`, `--network host`, `--cap-add`, `--device`, `--gpus` | Removed, with a note explaining why |
| `-d`, `-it`, `--rm` | Ignored |

## Docker image

Run any image, such as `nginx`, `postgres:17` or `ghcr.io/user/app:1.4`.

- **Port**: detected from the image's `EXPOSE` metadata. If there are several, you choose; if there are none, enter the port the app listens on.
- **Public**: on by default. The app gets `https://<name>-xxxx.<apps domain>`.
- **Private images**: add credentials in **Settings → Private registries** (Docker Hub, GHCR, GitLab or your own registry). Use an access token, not your password.
- Each deploy records the image **digest**, so a rollback runs exactly the same image even if the tag has moved since.
- Change the image later from the deployment's **Overview** (pencil icon next to the image).

## Dockerfile

Build and run your own code.

- **Upload a ZIP** of the project (Dockerfile plus source). A ZIP containing a single folder is handled automatically.
- **Git repository**: a public `https://` repository and branch. Click **Analyze** to find the Dockerfiles and their exposed ports.
- Builds use BuildKit, and their output streams to **Logs → Build**.
- Images are tagged `insta-deploy/<id>:r<revision>`. The last five are kept, so rolling back to a recent version doesn't rebuild.

## Docker Compose

Provide the project as pasted YAML (or an opened `compose.yaml`), an uploaded ZIP (Compose file plus Dockerfiles and config), or a Git repository.

Insta Deploy lists every service with its image or build folder and detected ports. **No service is public until you switch it on.** Each public service gets its own URL (`<name>-<service>-xxxx.<apps domain>`), and the others are reachable only from inside the project, by service name.

How it differs from plain `docker compose up`:

| Compose setting | In Insta Deploy |
| --- | --- |
| `ports: ["8080:80"]` | Removed. Make the service public instead. |
| Relative bind mounts (`./data:/data`) | Work; they point into the project folder on the machine |
| Named volumes | Work as usual (prefixed with the project name) |
| Host paths (`/srv/x:/x`) | Refused, unless the folder is in the agent's `INSTA_DEPLOY_ALLOWED_HOST_PATHS` |
| `privileged`, `network_mode: host`, `pid: host`, `cap_add`, `devices`, Docker socket | Refused |
| `env_file`, `include`, `extends` | Allowed for files inside the project |
| `${VAR}` | Filled from your Insta Deploy variables (and the project's `.env`) |

Changing which services are public triggers a redeploy, so services can join or leave the tunnel network.

## Variables and secrets

Variables can be set at four levels. The most specific one wins:

1. **Project**: every deployment in the project
2. **Project + environment**: e.g. only in `production`
3. **Deployment**: one deployment (its **Environment** tab)
4. **Service**: one Compose service

Project-level variables are under **Variables & Secrets** or on the project page.

A variable marked **secret** is:

- encrypted at rest (AES-256-GCM);
- never shown again or returned by the API;
- redacted as `••••••••` in build, deployment and container logs and in error messages.

Changes apply on the next deploy. Rollbacks use the current variables, because variables aren't versioned.

## Volumes, limits and restart policy

These options are for image and Dockerfile deployments. For Compose, set them in the Compose file.

- **Volumes**: a name and a path, e.g. `postgres-data` → `/var/lib/postgresql/data`. Data survives redeploys and rollbacks. On the machine, volumes are named `insta-<id>-<name>`, so deployments never share one by accident. When you delete a deployment, you choose whether to delete its volumes.
- **Limits**: CPU (e.g. `0.5`) and memory in MB, under **Advanced**.
- **Restart policy**: `unless-stopped` (the default), `always`, `on-failure` or `no`.

## Health checks

- Images with a Docker `HEALTHCHECK` report **Healthy**, **Unhealthy** or **Starting** automatically.
- Otherwise, add an **HTTP health check** (a path and an interval) on the service. Any response below 400 counts as healthy.

## Managing a deployment

Each deployment page has these tabs:

| Tab | What's there |
| --- | --- |
| Overview | Details, image or commit, current version, CPU, memory and network usage |
| Logs | **Build** and **Deployment** logs (live), **Container** logs (last 300 lines, optional auto refresh) |
| Environment | The deployment's variables and secrets |
| Services | Containers, state and health; make services public or private, change ports, set health checks |
| Domains | Generated URLs and custom domains |
| History | Every revision: trigger, status, digest or commit; **Rollback** to any earlier one |

The header buttons are **Open**, **Start/Stop**, **Redeploy**, and in the **⋯** menu, **Restart** and **Delete**.

- **Auto deploy** (Git sources): turn it on in **Overview**. The branch is checked every 60 seconds (`GIT_POLL_SECONDS`), and new commits are deployed.
- **Rollback** creates a new revision from an earlier one; it doesn't rewrite history.

## Custom domains

1. **Domains → Add domain**, enter e.g. `app.example.com`, and choose the service.
2. Create the DNS records shown at your DNS provider.
3. Insta Deploy checks every 30 seconds. Once the records are verified, the domain is routed to the service with HTTPS.

You can point a domain at a different service at any time. See [pangolin.md](pangolin.md#custom-domains) for the record types.

## Troubleshooting

| Message | What to do |
| --- | --- |
| *Unable to pull image …* | Check the name and tag. For private images, add registry credentials. |
| *Docker build failed* | Open **Logs → Build**. |
| *The container exited right after starting* | Open **Logs → Container**; it's usually missing configuration. |
| *This machine is currently offline* | Start the agent on that machine. Queued work runs when it reconnects. |
| *Public route could not be created* | The reason is shown on the **Services** tab. Fix it and click **Retry**. |
| *The Compose file uses settings that aren't allowed* | The message lists each one; see the Compose table above. |
