# Pangolin setup

Insta Deploy uses [Pangolin](https://github.com/fosrl/pangolin) to give apps public HTTPS URLs. Pangolin is a tunneled reverse proxy: each machine opens an **outbound** tunnel to it, and Pangolin serves your apps on the internet. You don't need port forwarding, a public IP or certificate management on your side.

```
visitor ──HTTPS──▶ Pangolin ══ tunnel (outbound from your machine) ══▶ newt ──▶ container
```

You can use **Pangolin Cloud** (hosted, with a free tier) or **self-host Pangolin** on a small VPS. Both work the same way. Insta Deploy always uses your own Pangolin organization, never a shared one.

Without Pangolin, apps still run, but they have no public URL.

## What Insta Deploy manages for you

Once connected, you never need to touch Pangolin again:

| In Insta Deploy | In Pangolin |
| --- | --- |
| Connect a machine | Creates a **site** `insta-deploy: <machine>`; the agent starts `newt` for it |
| A public service starts running | Creates a **resource** `<name>-xxxx.<apps domain>` with a **target** pointing at the container, with Pangolin's login turned off |
| Change a service's port | Updates the target |
| Make a service private, or delete it | Deletes the resource |
| Add a custom domain | Registers the **domain**, then creates a resource once DNS is verified |
| Delete a machine | Deletes its resources and its site |

## Option A: Pangolin Cloud

1. **Create an account and organization** at [app.pangolin.net](https://app.pangolin.net). The organization ID is in the URL (e.g. `org_abc123xyz`).

2. **Add a domain for your apps.** Use a subdomain such as `apps.example.com` so the rest of your domain is unaffected. In Pangolin, go to **Domains → Add Domain**, choose **Domain Delegation (NS)**, and enter `apps.example.com`. Then create the NS records it shows at your DNS provider:

   ```
   NS  apps  ns1.pangolin-ns.net
   NS  apps  ns2.pangolin-ns.net
   NS  apps  ns3.pangolin-ns.net
   ```

   Where to add them at common DNS providers (use **Name/Host** `apps` and add one NS record per nameserver):

   | Provider | Where |
   | --- | --- |
   | Cloudflare | DNS → Records → Add record → Type **NS** |
   | GoDaddy | Domains → DNS → Add New Record → Type **NS** (don't use *Change Nameservers*) |
   | Namecheap | Domain List → Manage → Advanced DNS → Add New Record → **NS Record** |
   | AWS Route 53 | Hosted zones → your domain → Create record → Type **NS** (all three values, one per line) |
   | Squarespace (Google Domains) | DNS → DNS Settings → Custom records → Type **NS** |
   | Hostinger | DNS / Nameservers → DNS records → Type **NS** |

   Remove any existing A, AAAA or CNAME record for `apps` first, and never change the nameservers of the whole domain. Check with `dig NS apps.example.com +short @1.1.1.1`, which should list the three Pangolin nameservers. The setup guide has the same instructions with copy buttons.

   Wait until the domain shows **Verified**. Delegation lets every new deployment hostname work instantly, under one wildcard certificate.

3. **Create an API key** under **API Keys** with the [permissions below](#api-key-permissions).

4. **Connect it in Insta Deploy.** Open the setup guide (the **?** button in the header), go to the **Public URLs** step, choose **Pangolin Cloud**, and enter the organization ID and API key. Click **Check**, pick `apps.example.com`, and click **Save**.

**Free plan limits** (at the time of writing; see [pricing](https://pangolin.net/pricing)): 5 sites, 15 public resources, 5 domains, 5 users, 1 organization. In Insta Deploy terms, that's **5 machines**, **15 public URLs** (generated and custom together) and **4 custom domains** (the apps domain uses one). The dashboard shows a collapsible note with your current usage on the Machines and Domains pages.

## Option B: Self-hosted Pangolin

### 1. Server and DNS

You need a Linux server with a public IP, with ports **80/tcp, 443/tcp, 51820/udp and 21820/udp** open, and a domain with these records pointing at the server:

| Record | Type | Purpose |
| --- | --- | --- |
| `pangolin.example.com` | A | Dashboard and tunnels |
| `api.example.com` | A | Integration API |
| `*.apps.example.com` | A | Deployment URLs |

### 2. Install Pangolin

Follow the [official quick install](https://docs.pangolin.net/self-host/quick-install):

```bash
curl -fsSL https://static.pangolin.net/get-installer.sh | bash
sudo ./installer
```

Use base domain `example.com` and dashboard domain `pangolin.example.com`. Then open `https://pangolin.example.com/auth/initial-setup`, create the admin account (the setup token is in `sudo docker compose logs pangolin`), and create an organization.

### 3. Add the apps domain

In Pangolin's `config/config.yml`:

```yaml
domains:
  domain1:
    base_domain: "example.com"
    cert_resolver: "letsencrypt"
  apps:
    base_domain: "apps.example.com"
    cert_resolver: "letsencrypt"
```

Restart Pangolin and check that `apps.example.com` appears under **Domains**.

### 4. Enable the Integration API

In `config/config.yml` ([docs](https://docs.pangolin.net/self-host/advanced/integration-api)):

```yaml
flags:
  enable_integration_api: true
```

Expose it at `api.example.com` in `config/traefik/dynamic_config.yml`:

```yaml
http:
  routers:
    int-api-router-redirect:
      rule: "Host(`api.example.com`)"
      service: int-api-service
      entryPoints: [web]
      middlewares: [redirect-to-https]
    int-api-router:
      rule: "Host(`api.example.com`)"
      service: int-api-service
      entryPoints: [websecure]
      tls:
        certResolver: letsencrypt
  services:
    int-api-service:
      loadBalancer:
        servers:
          - url: "http://pangolin:3003"
```

Restart Pangolin. `https://api.example.com/v1/docs` should show the API docs.

### 5. Connect it in Insta Deploy

Create an organization API key with the [permissions below](#api-key-permissions). In the setup guide's **Public URLs** step, choose **Self-hosted Pangolin** and enter:

- **Dashboard URL**: `https://pangolin.example.com`
- **API URL**: `https://api.example.com/v1`
- **Organization ID** and **API key**

Click **Check**, pick the apps domain, and click **Save**.

## API key permissions

| Area | Permissions | Used for |
| --- | --- | --- |
| Sites | create, get, delete | One tunnel per machine |
| Resources | create, get, update, delete | Public URLs |
| Targets | create, list, update | Pointing URLs at containers |
| Domains | list, create, get, delete, restart | Apps domain and custom domains |

The key is stored encrypted, and is never shown again after saving.

## Custom domains

**Domains → Add domain** registers a domain such as `app.example.com` with Pangolin and shows the DNS records to create:

- **Pangolin Cloud**: two CNAME records, one for the domain and one for `_acme-challenge.` (the certificate).
- **Self-hosted**: an A record pointing at your Pangolin server.

Insta Deploy checks DNS every 30 seconds through public DNS-over-HTTPS resolvers, so your local DNS cache doesn't delay verification. **Check again** forces an immediate check. Once verified, the domain is routed to its service and the SSL status appears after the certificate is issued.

## Troubleshooting

| Problem | Fix |
| --- | --- |
| Machine shows **Tunnel: Not configured** | The error under it comes from Pangolin. Usually it's a missing API key permission. Machines connected before Pangolin pick up their tunnel within a minute. |
| Apps domain isn't in the list | Add it in Pangolin (**Domains**), then click **Check** again. |
| **Public URL failed** | The Services tab shows Pangolin's error. Fix it, then click **Retry**. |
| Certificate warning on a new URL | The first certificate for a new domain can take a minute. With NS delegation, later URLs are instant. |
| URL returns 502 or 404 | On the machine, run `docker logs insta-deploy-newt` to check the tunnel. Make sure the app listens on the chosen port (**Logs → Container**) and that the service is public. |
| Custom domain stuck on "Waiting for DNS" | Check the records with `dig CNAME app.example.com @1.1.1.1`. A conflicting A record or a proxied (orange-cloud) record blocks verification. |
