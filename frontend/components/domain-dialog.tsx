"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { Deployment, Domain } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SimpleSelect } from "@/components/simple-select";
import { DnsRecords } from "@/components/dns-records";

// Add a custom domain and (optionally) point it at a service. After
// creating, shows the DNS records to add.
export function AddDomainDialog({
  open,
  onOpenChange,
  defaultServiceId,
  onAdded,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultServiceId?: string;
  onAdded?: () => void;
}) {
  const [hostname, setHostname] = useState("");
  const [serviceId, setServiceId] = useState(defaultServiceId ?? "");
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [saving, setSaving] = useState(false);
  const [created, setCreated] = useState<Domain | null>(null);

  useEffect(() => {
    if (!open) return;
    api<Deployment[]>("/deployments")
      .then(setDeployments)
      .catch(() => {});
  }, [open]);

  const options = deployments.flatMap((d) =>
    d.services
      .filter((s) => s.port)
      .map((s) => ({
        value: s.id,
        label:
          d.type === "COMPOSE"
            ? `${d.name} / ${s.name} (:${s.port})`
            : `${d.name} (:${s.port})`,
      })),
  );

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      const d = await api<Domain>("/domains", {
        method: "POST",
        body: { hostname: hostname.trim(), service_id: serviceId || undefined },
      });
      setCreated(d);
      onAdded?.();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  function close(o: boolean) {
    if (!o) {
      setCreated(null);
      setHostname("");
    }
    onOpenChange(o);
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-2xl">
        {created ? (
          <div className="min-w-0 space-y-4">
            <DialogHeader>
              <DialogTitle>Add these DNS records</DialogTitle>
              <DialogDescription>
                At the company that manages DNS for{" "}
                <strong>{created.hostname}</strong>, create the records below.
                Insta Deploy checks every 30 seconds and connects the domain
                once they&apos;re visible.
              </DialogDescription>
            </DialogHeader>
            <DnsRecords records={created.dns_records} />
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            <DialogHeader>
              <DialogTitle>Add a custom domain</DialogTitle>
              <DialogDescription>
                Use your own domain, like app.example.com, for a deployment.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor="domain-host">Domain</Label>
              <Input
                id="domain-host"
                required
                autoFocus
                className="font-mono"
                placeholder="app.example.com"
                value={hostname}
                onChange={(e) => setHostname(e.target.value.toLowerCase())}
              />
            </div>
            <div className="space-y-2">
              <Label>Points to</Label>
              <SimpleSelect
                value={serviceId}
                onChange={setServiceId}
                placeholder="Choose later"
                options={[{ value: "", label: "Nothing yet" }, ...options]}
              />
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => close(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={saving || !hostname}>
                {saving && <Loader2 className="animate-spin" />} Add domain
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
