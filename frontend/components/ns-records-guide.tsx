"use client";

import { useState } from "react";
import { BookOpen, ChevronDown, TriangleAlert } from "lucide-react";
import { cn } from "@/lib/utils";
import { CopyButton } from "@/components/copy-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Pangolin Cloud's nameservers for delegated domains. Pangolin shows the
// same values on the domain's page; those always take precedence.
const nameservers = [
  "ns1.pangolin-ns.net",
  "ns2.pangolin-ns.net",
  "ns3.pangolin-ns.net",
];

const providers: { id: string; name: string; steps: React.ReactNode[] }[] = [
  {
    id: "cloudflare",
    name: "Cloudflare",
    steps: [
      <>
        Open your domain and go to <b>DNS → Records</b>.
      </>,
      <>
        Click <b>Add record</b>, set <b>Type</b> to <b>NS</b>, <b>Name</b> to
        the host below, and <b>Nameserver</b> to the first value.
      </>,
      <>Save, then repeat for the other two nameservers (same name).</>,
      <>
        NS records can&apos;t be proxied, so there&apos;s no orange cloud to
        switch off.
      </>,
    ],
  },
  {
    id: "godaddy",
    name: "GoDaddy",
    steps: [
      <>
        Go to <b>My Products → Domains</b>, pick the domain, and open <b>DNS</b>{" "}
        (DNS Records).
      </>,
      <>
        Click <b>Add New Record</b>, choose type <b>NS</b>, enter the host below
        as <b>Name</b> and the first nameserver as <b>Value</b>. Leave TTL at 1
        hour.
      </>,
      <>Save, then add the other two nameservers the same way.</>,
      <>
        Don&apos;t use <b>Change Nameservers</b>: that moves your whole domain
        away from GoDaddy.
      </>,
    ],
  },
  {
    id: "namecheap",
    name: "Namecheap",
    steps: [
      <>
        Go to <b>Domain List → Manage</b> and open the <b>Advanced DNS</b> tab.
      </>,
      <>
        Under <b>Host Records</b>, click <b>Add New Record</b> and choose{" "}
        <b>NS Record</b>.
      </>,
      <>
        Enter the host below as <b>Host</b> and the first nameserver as{" "}
        <b>Value</b>, then click the ✓ to save.
      </>,
      <>Repeat for the other two nameservers.</>,
    ],
  },
  {
    id: "route53",
    name: "AWS Route 53",
    steps: [
      <>
        Open <b>Hosted zones</b> and select your domain.
      </>,
      <>
        Click <b>Create record</b>, enter the host below as <b>Record name</b>,
        and choose type <b>NS</b>.
      </>,
      <>
        Paste all three nameservers into <b>Value</b>, one per line, and click{" "}
        <b>Create records</b>.
      </>,
    ],
  },
  {
    id: "squarespace",
    name: "Squarespace (Google Domains)",
    steps: [
      <>
        Open <b>Domains</b>, pick the domain, and go to{" "}
        <b>DNS → DNS Settings</b>.
      </>,
      <>
        Under <b>Custom records</b>, click <b>Add record</b> and choose type{" "}
        <b>NS</b>.
      </>,
      <>
        Enter the host below and the first nameserver, save, then add the other
        two.
      </>,
    ],
  },
  {
    id: "hostinger",
    name: "Hostinger",
    steps: [
      <>
        Open <b>Domains</b>, pick the domain, and go to{" "}
        <b>DNS / Nameservers → DNS records</b>.
      </>,
      <>
        Choose type <b>NS</b>, enter the host below as <b>Name</b> and the first
        nameserver as <b>Points to</b>, and click <b>Add Record</b>.
      </>,
      <>Repeat for the other two nameservers.</>,
    ],
  },
  {
    id: "other",
    name: "Other",
    steps: [
      <>
        Open the DNS settings (often called <b>DNS Management</b>,{" "}
        <b>DNS Zone</b> or <b>Advanced DNS</b>) for your domain at the company
        that hosts its DNS.
      </>,
      <>
        Add a record of type <b>NS</b> for the host below, pointing at the first
        nameserver.
      </>,
      <>
        Add the other two nameservers as separate NS records with the same host.
      </>,
      <>
        Don&apos;t change the domain&apos;s own nameservers: only the subdomain
        is delegated.
      </>,
    ],
  },
];

// Step-by-step help for delegating the apps subdomain to Pangolin Cloud
// with NS records, at the user's own DNS provider.
export function NsRecordsGuide() {
  const [domain, setDomain] = useState("apps.yourdomain.com");
  const [provider, setProvider] = useState("cloudflare");

  const clean = domain
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, "")
    .replace(/\/.*$/, "")
    .replace(/\.$/, "");
  const labels = clean.split(".").filter(Boolean);
  // "apps.example.com" -> host "apps" in the "example.com" zone. Multi-part
  // suffixes (example.co.uk) need the user to adjust, which the note says.
  const host = labels.length > 2 ? labels.slice(0, -2).join(".") : "apps";
  const zone =
    labels.length >= 2 ? labels.slice(-2).join(".") : "yourdomain.com";
  const current = providers.find((p) => p.id === provider) ?? providers[0];

  return (
    <details className="group rounded-xl border bg-card text-sm text-foreground">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-4 py-3 font-medium select-none [&::-webkit-details-marker]:hidden">
        <BookOpen className="size-4 shrink-0 text-primary" />
        <span className="flex-1">
          How to add the NS records at your DNS provider
        </span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
      </summary>

      <div className="space-y-5 border-t px-4 py-4">
        <p className="text-muted-foreground">
          NS records hand one subdomain (and everything under it) over to
          Pangolin, while the rest of your domain, its website and email stay
          exactly where they are.
        </p>

        <div className="space-y-1.5">
          <Label htmlFor="ns-domain">Your apps domain</Label>
          <Input
            id="ns-domain"
            className="font-mono sm:max-w-sm"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="apps.yourdomain.com"
          />
          <p className="text-xs text-muted-foreground">
            The same subdomain you entered in Pangolin. Records go in the DNS of{" "}
            <code>{zone}</code>.
          </p>
        </div>

        <div className="space-y-2">
          <p className="font-medium">Records to add</p>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-left font-mono text-xs">
              <thead className="bg-muted/50 font-sans text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 font-medium">Type</th>
                  <th className="px-3 py-2 font-medium">Name / Host</th>
                  <th className="px-3 py-2 font-medium">Value / Nameserver</th>
                  <th className="px-3 py-2 font-medium">TTL</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {nameservers.map((ns) => (
                  <tr key={ns}>
                    <td className="px-3 py-2">NS</td>
                    <td className="px-3 py-2">
                      <span className="inline-flex items-center gap-1">
                        {host}
                        <CopyButton value={host} className="size-6" />
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      <span className="inline-flex items-center gap-1">
                        {ns}
                        <CopyButton value={ns} className="size-6" />
                      </span>
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">
                      Default / 3600
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="text-xs text-muted-foreground">
            Use the nameservers Pangolin shows on the domain&apos;s page if they
            differ. Some providers want the full name (
            <code>{clean || "apps.yourdomain.com"}</code>) instead of just{" "}
            <code>{host}</code>; they usually show which in the form.
          </p>
        </div>

        <div className="space-y-3">
          <p className="font-medium">Step by step</p>
          <div
            className="flex flex-wrap gap-1.5"
            role="tablist"
            aria-label="DNS provider"
          >
            {providers.map((p) => (
              <button
                key={p.id}
                type="button"
                role="tab"
                aria-selected={provider === p.id}
                onClick={() => setProvider(p.id)}
                className={cn(
                  "rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                  provider === p.id
                    ? "border-primary bg-primary text-primary-foreground"
                    : "bg-card text-muted-foreground hover:text-foreground",
                )}
              >
                {p.name}
              </button>
            ))}
          </div>
          <ol className="space-y-2" role="tabpanel">
            {current.steps.map((step, i) => (
              <li key={i} className="flex gap-2.5">
                <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-[11px] font-semibold">
                  {i + 1}
                </span>
                <span className="text-muted-foreground [&_b]:font-medium [&_b]:text-foreground">
                  {step}
                </span>
              </li>
            ))}
          </ol>
        </div>

        <div className="space-y-2">
          <p className="font-medium">Check it worked</p>
          <p className="text-muted-foreground">
            Changes usually show up within minutes, sometimes up to a few hours.
            In a terminal, this should list the three Pangolin nameservers:
          </p>
          <div className="flex items-center gap-1 rounded-lg bg-zinc-950 py-1 pr-1 pl-3 font-mono text-xs text-zinc-100">
            <span className="flex-1 truncate">
              dig NS {clean || "apps.yourdomain.com"} +short @1.1.1.1
            </span>
            <CopyButton
              value={`dig NS ${clean || "apps.yourdomain.com"} +short @1.1.1.1`}
              className="text-zinc-300 hover:bg-zinc-800 hover:text-white"
            />
          </div>
          <p className="text-muted-foreground">
            Then wait for the domain to show{" "}
            <b className="font-medium text-foreground">Verified</b> in Pangolin
            before you continue.
          </p>
        </div>

        <div className="flex gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-xs">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-[oklch(0.55_0.13_70)] dark:text-warning" />
          <ul className="list-disc space-y-1 pl-4">
            <li>
              Remove any existing <b>A</b>, <b>AAAA</b> or <b>CNAME</b> record
              for <code>{host}</code> (or a wildcard <code>*.{host}</code>)
              first: they conflict with NS records.
            </li>
            <li>
              Only add NS records for the subdomain. Never change the
              nameservers of {zone} itself.
            </li>
            <li>
              For domains like <code>example.co.uk</code>, the host is
              everything before your registered domain (e.g. <code>apps</code>).
            </li>
          </ul>
        </div>
      </div>
    </details>
  );
}
