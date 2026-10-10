"use client";

import { useState } from "react";
import {
  ChevronDown,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Loader2,
  LockKeyhole,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
  Shuffle,
} from "lucide-react";
import type { AccessMode, AuthHint } from "@/lib/types";
import { cn } from "@/lib/utils";
import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export interface AccessChoice {
  mode: AccessMode;
  // Empty keeps the current password or PIN (when editing).
  secret: string;
}

const authInfo: Record<
  AuthHint | "unknown",
  { icon: typeof ShieldCheck; tone: string; title: string; text: string }
> = {
  login: {
    icon: ShieldCheck,
    tone: "border-success/30 bg-success/5 [&_svg]:text-success",
    title: "This app has its own login",
    text: "Visitors must sign in to the app before they can see or change anything. Use a strong password for its accounts.",
  },
  setup: {
    icon: ShieldAlert,
    tone: "border-warning/40 bg-warning/10 [&_svg]:text-[oklch(0.55_0.13_70)] dark:[&_svg]:text-warning",
    title: "Whoever opens it first can take it over",
    text: "The app is unclaimed until someone completes its setup or creates the first admin account. Open the URL right after it starts and finish setup yourself, or add protection below.",
  },
  none: {
    icon: ShieldAlert,
    tone: "border-destructive/30 bg-destructive/5 [&_svg]:text-destructive",
    title: "This app has no login",
    text: "Anyone who finds the URL can use it. Add a password or PIN below unless it's meant to be fully public.",
  },
  unknown: {
    icon: ShieldQuestion,
    tone: "border-info/25 bg-info/5 [&_svg]:text-info",
    title: "Insta Deploy can't tell whether this app has a login",
    text: "Check the app's documentation. If it doesn't ask visitors to sign in, anyone who finds the URL can use it. Add a password or PIN below if you're not sure.",
  },
};

function randomPassword() {
  const chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => chars[b % chars.length]).join("");
}

function randomPin() {
  const bytes = crypto.getRandomValues(new Uint32Array(1));
  return String(bytes[0] % 1_000_000).padStart(6, "0");
}

// Asks before a service gets a public URL: explains what public means,
// whether the app protects itself, and offers a Pangolin password or PIN.
// With editing, it changes the protection of an already public service.
export function PublicAccessDialog({
  open,
  onOpenChange,
  serviceName,
  authHint,
  authNote,
  editing,
  currentAccess = "none",
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serviceName: string;
  authHint?: AuthHint;
  authNote?: string;
  editing?: boolean;
  currentAccess?: AccessMode;
  onConfirm: (choice: AccessChoice) => void | Promise<void>;
}) {
  // Default to protection when the app can't protect itself.
  const initial: AccessMode = editing
    ? currentAccess
    : authHint === "none" || authHint === "setup"
      ? "password"
      : "none";
  const [mode, setMode] = useState<AccessMode>(initial);
  const [secret, setSecret] = useState(() =>
    initial === "password" && !editing ? randomPassword() : "",
  );
  const [shown, setShown] = useState(!editing);
  const [busy, setBusy] = useState(false);
  const info = authInfo[authHint ?? "unknown"];

  const keeping =
    editing && mode === currentAccess && mode !== "none" && secret === "";
  const valid =
    mode === "none" ||
    keeping ||
    (mode === "password"
      ? secret.length >= 8 && secret.length <= 100
      : /^\d{6}$/.test(secret));

  function choose(m: AccessMode) {
    setMode(m);
    if (editing && m === currentAccess) setSecret("");
    else
      setSecret(
        m === "password"
          ? randomPassword()
          : m === "pincode"
            ? randomPin()
            : "",
      );
    setShown(true);
  }

  async function confirm() {
    setBusy(true);
    try {
      await onConfirm({ mode, secret: mode === "none" ? "" : secret });
      onOpenChange(false);
    } catch {
      // The caller showed the error; keep the dialog open to retry.
    } finally {
      setBusy(false);
    }
  }

  const options: {
    value: AccessMode;
    icon: typeof Globe;
    title: string;
    text: string;
  }[] = [
    {
      value: "none",
      icon: Globe,
      title: "No extra protection",
      text: "Rely on the app's own login.",
    },
    {
      value: "password",
      icon: KeyRound,
      title: "Password",
      text: "Pangolin asks for a password first.",
    },
    {
      value: "pincode",
      icon: LockKeyhole,
      title: "6-digit PIN",
      text: "Quick to type on a phone.",
    },
  ];

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Globe className="size-5 text-primary" />
            {editing
              ? `Access protection for ${serviceName}`
              : `Make ${serviceName} public?`}
          </DialogTitle>
          <DialogDescription>
            {editing
              ? "Choose what Pangolin asks for before anyone reaches this service."
              : "It gets an HTTPS address that works from anywhere on the internet."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          {!editing && (
            <ul className="space-y-1.5 rounded-lg border bg-muted/30 p-3 text-muted-foreground">
              <li>
                • Anyone who has or finds the URL can reach it, not just you.
                Automated scanners look for new apps within minutes.
              </li>
              <li>
                • Your machine still opens no ports: traffic arrives through the
                outbound Pangolin tunnel.
              </li>
              <li>
                • You can make it private again at any time; the URL then stops
                working.
              </li>
            </ul>
          )}

          <div className={cn("flex gap-3 rounded-lg border p-3", info.tone)}>
            <info.icon className="mt-0.5 size-5 shrink-0" />
            <div className="space-y-1">
              <p className="font-medium">{info.title}</p>
              <p className="text-muted-foreground">{info.text}</p>
              {authNote && <p className="text-muted-foreground">{authNote}</p>}
            </div>
          </div>

          <div className="space-y-2">
            <p className="font-medium">Access protection</p>
            <div
              className="grid gap-2 sm:grid-cols-3"
              role="radiogroup"
              aria-label="Access protection"
            >
              {options.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  role="radio"
                  aria-checked={mode === o.value}
                  onClick={() => choose(o.value)}
                  className={cn(
                    "flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors hover:border-ring/40",
                    mode === o.value &&
                      "border-primary bg-accent/50 ring-1 ring-primary/30",
                  )}
                >
                  <span className="flex items-center gap-1.5 font-medium">
                    <o.icon className="size-4 text-primary" /> {o.title}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {o.text}
                  </span>
                </button>
              ))}
            </div>

            {mode !== "none" && (
              <div className="space-y-1.5 pt-1">
                <div className="flex items-center gap-1">
                  <Input
                    className="font-mono"
                    type={shown ? "text" : "password"}
                    inputMode={mode === "pincode" ? "numeric" : undefined}
                    maxLength={mode === "pincode" ? 6 : 100}
                    autoComplete="new-password"
                    placeholder={
                      keeping
                        ? "Unchanged (type to replace)"
                        : mode === "pincode"
                          ? "6 digits"
                          : "At least 8 characters"
                    }
                    value={secret}
                    onChange={(e) =>
                      setSecret(
                        mode === "pincode"
                          ? e.target.value.replace(/\D/g, "")
                          : e.target.value,
                      )
                    }
                    aria-label={mode === "pincode" ? "PIN" : "Password"}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setShown(!shown)}
                    aria-label={shown ? "Hide" : "Show"}
                  >
                    {shown ? <EyeOff /> : <Eye />}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => {
                      setSecret(
                        mode === "pincode" ? randomPin() : randomPassword(),
                      );
                      setShown(true);
                    }}
                    aria-label="Generate"
                    title="Generate"
                  >
                    <Shuffle />
                  </Button>
                  {secret && <CopyButton value={secret} />}
                </div>
                <p className="text-xs text-muted-foreground">
                  Save it now: it&apos;s stored encrypted and can&apos;t be
                  shown again. Browsers get a Pangolin sign-in page;{" "}
                  <b className="font-medium text-foreground">
                    mobile apps, API clients and webhooks can&apos;t pass it
                  </b>
                  , so don&apos;t protect services they need to reach.
                </p>
              </div>
            )}
          </div>

          <details className="group rounded-lg border">
            <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs font-medium select-none [&::-webkit-details-marker]:hidden">
              <LockKeyhole className="size-3.5 text-muted-foreground" />
              <span className="flex-1">How the connection is encrypted</span>
              <ChevronDown className="size-3.5 text-muted-foreground transition-transform group-open:rotate-180" />
            </summary>
            <ol className="space-y-1 border-t px-3 py-2 text-xs text-muted-foreground">
              <li>
                <b className="font-medium text-foreground">
                  1. Visitor → Pangolin:
                </b>{" "}
                HTTPS. TLS ends at Pangolin, which holds the certificate.
              </li>
              <li>
                <b className="font-medium text-foreground">
                  2. Pangolin → your machine:
                </b>{" "}
                inside the WireGuard tunnel (newt), encrypted end to end.
              </li>
              <li>
                <b className="font-medium text-foreground">
                  3. newt → container:
                </b>{" "}
                plain HTTP on a private Docker network, never leaving your
                machine.
              </li>
              <li className="pt-1">
                Pangolin decrypts traffic in step 1, so it can see requests. Use
                a self-hosted Pangolin if that matters to you.
              </li>
            </ol>
          </details>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            Cancel
          </Button>
          <Button onClick={confirm} disabled={!valid || busy}>
            {busy && <Loader2 className="animate-spin" />}
            {editing
              ? "Save"
              : mode === "none"
                ? "Make public"
                : mode === "password"
                  ? "Make public with password"
                  : "Make public with PIN"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
