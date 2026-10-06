# Upgrading

## Any upgrade

1. **Back up** the database and the secrets key:

   ```bash
   docker compose exec -T postgres pg_dump -U insta insta > backup.sql
   docker run --rm -v instadeploy_backend-data:/data -v "$PWD":/out alpine \
     tar czf /out/backend-data.tgz -C /data .
   ```

   The `backend-data` volume holds the key that encrypts your secrets. A database backup without it can't decrypt them. (The volume name is prefixed with your Compose project name, `instadeploy` by default.)

2. **Update and restart**:

   ```bash
   git pull
   docker compose up -d --build
   ```

   The `migrate` service applies any new database migrations before the API starts. Running deployments are not affected.

3. **Update the agents** if the agent changed. `docker compose up --build` rebuilds the agent image on the server. Then restart the agent container on each machine. **Machines → ⋯ → Issue new token** shows the full `docker run` command. Deployments keep running while the agent restarts, and any interrupted work is resumed.

## Restoring a backup

```bash
docker compose down
docker volume rm instadeploy_pgdata
docker compose up -d postgres
docker compose exec -T postgres psql -U insta insta < backup.sql
docker run --rm -v instadeploy_backend-data:/data -v "$PWD":/in alpine \
  tar xzf /in/backend-data.tgz -C /data
docker compose up -d
```

## From 0.1 to 0.2

0.2 added projects, Dockerfile and Compose deployments, logs, secrets, custom domains, history and Better Auth. Your data is kept:

- Accounts move to Better Auth; existing passwords keep working.
- Existing deployments land in a project called **Default**, keeping their container names and public URLs.
- The old agent doesn't understand the new task format, so **restart every agent** with the new image. Its `docker run` command must include `-v /var/lib/insta-deploy:/var/lib/insta-deploy`; the command shown in the dashboard does.
- Pangolin API keys need the extra permissions listed in [pangolin.md](pangolin.md#api-key-permissions) (targets list/update, domains).

## Settings moved from `.env` to the dashboard

Pangolin, the apps domain and the server address are now edited in the dashboard (setup guide and **Settings**). On first start, values found in `.env` are imported once, and after that `.env` is optional. If you remove a `.env` that held `SESSION_SECRET`, everyone simply signs in again.
