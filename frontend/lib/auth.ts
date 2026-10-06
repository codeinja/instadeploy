import "server-only";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { betterAuth } from "better-auth";
import { nextCookies } from "better-auth/next-js";
import { verifyPassword } from "better-auth/crypto";
import bcrypt from "bcryptjs";
import { Pool } from "pg";

// Better Auth owns sign-up, sign-in, sessions and passwords. It stores them
// in Postgres ("user", "session", "account", "verification" tables, created
// by backend/migrations/002_better_auth.sql). The Go backend authenticates
// API requests by looking up the same session token.

// Signs session cookies. Use BETTER_AUTH_SECRET (or the older
// SESSION_SECRET); otherwise generate one once and keep it on disk so
// sessions survive restarts.
function authSecret(): string {
  const fromEnv = process.env.BETTER_AUTH_SECRET || process.env.SESSION_SECRET;
  if (fromEnv && fromEnv !== "change-me") return fromEnv;
  // Docker sets AUTH_SECRET_FILE to a volume. Local dev falls back to tmp.
  const file = process.env.AUTH_SECRET_FILE ?? path.join(os.tmpdir(), "insta-deploy-auth-secret");
  try {
    return fs.readFileSync(/* turbopackIgnore: true */ file, "utf8").trim();
  } catch {
    const secret = crypto.randomBytes(32).toString("hex");
    fs.mkdirSync(/* turbopackIgnore: true */ path.dirname(file), { recursive: true });
    fs.writeFileSync(/* turbopackIgnore: true */ file, secret, { mode: 0o600 });
    return secret;
  }
}

export const auth = betterAuth({
  database: new Pool({
    connectionString: process.env.DATABASE_URL ?? "postgres://insta:insta@localhost:5432/insta",
  }),
  secret: authSecret(),
  // Set BETTER_AUTH_URL in production (behind HTTPS). Without it, the
  // dashboard works at whatever address it's opened with, so a plain
  // `docker compose up` needs no configuration.
  baseURL: process.env.BETTER_AUTH_URL || { allowedHosts: ["*"], protocol: "auto" },
  trustedOrigins: (process.env.BETTER_AUTH_TRUSTED_ORIGINS ?? "").split(",").filter(Boolean),
  advanced: {
    // Secure (HTTPS-only) cookies when the dashboard has an https:// URL.
    // Without one, it may be opened over plain http on a local network,
    // where browsers would refuse a secure cookie and sign-in would fail.
    useSecureCookies: (process.env.BETTER_AUTH_URL ?? "").startsWith("https://"),
  },
  emailAndPassword: {
    enabled: true,
    minPasswordLength: 8,
    password: {
      // Accounts created before Better Auth have bcrypt hashes ("$2...").
      verify: async ({ hash, password }) =>
        hash.startsWith("$2") ? bcrypt.compare(password, hash) : verifyPassword({ hash, password }),
    },
  },
  session: {
    expiresIn: 60 * 60 * 24 * 7, // 7 days
    updateAge: 60 * 60 * 24, // extend at most once a day
  },
  rateLimit: {
    enabled: true,
    window: 60,
    max: 100,
    customRules: {
      "/sign-in/email": { window: 60, max: 5 },
      "/sign-up/email": { window: 60, max: 3 },
      "/change-password": { window: 60, max: 5 },
    },
  },
  // OAuth providers (GitHub, Google) can be added here later via
  // `socialProviders` without touching the rest of the app.
  plugins: [nextCookies()],
});

export type Session = typeof auth.$Infer.Session;
