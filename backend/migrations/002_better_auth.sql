-- Authentication moves to Better Auth (running in the Next.js frontend).
-- These are the tables Better Auth expects, generated with its
-- getMigrations() for Postgres. Better Auth owns them; the Go backend only
-- reads "session" to authenticate API requests.

CREATE TABLE "user" (
    "id" text NOT NULL PRIMARY KEY,
    "name" text NOT NULL,
    "email" text NOT NULL UNIQUE,
    "emailVerified" boolean NOT NULL,
    "image" text,
    "createdAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE "session" (
    "id" text NOT NULL PRIMARY KEY,
    "expiresAt" timestamptz NOT NULL,
    "token" text NOT NULL UNIQUE,
    "createdAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamptz NOT NULL,
    "ipAddress" text,
    "userAgent" text,
    "userId" text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE
);

CREATE TABLE "account" (
    "id" text NOT NULL PRIMARY KEY,
    "accountId" text NOT NULL,
    "providerId" text NOT NULL,
    "userId" text NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "accessToken" text,
    "refreshToken" text,
    "idToken" text,
    "accessTokenExpiresAt" timestamptz,
    "refreshTokenExpiresAt" timestamptz,
    "scope" text,
    "password" text,
    "createdAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamptz NOT NULL
);

CREATE TABLE "verification" (
    "id" text NOT NULL PRIMARY KEY,
    "identifier" text NOT NULL,
    "value" text NOT NULL,
    "expiresAt" timestamptz NOT NULL,
    "createdAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE INDEX "session_userId_idx" ON "session" ("userId");
CREATE INDEX "account_userId_idx" ON "account" ("userId");
CREATE INDEX "verification_identifier_idx" ON "verification" ("identifier");

-- Move existing accounts over. Their bcrypt hashes keep working: the
-- frontend's Better Auth config verifies "$2..." hashes with bcrypt.
INSERT INTO "user" ("id", "name", "email", "emailVerified", "createdAt", "updatedAt")
SELECT id::text, split_part(email, '@', 1), email, false, created_at, created_at FROM users;

INSERT INTO "account" ("id", "accountId", "providerId", "userId", "password", "createdAt", "updatedAt")
SELECT gen_random_uuid()::text, id::text, 'credential', id::text, password_hash, created_at, created_at FROM users;

ALTER TABLE agents DROP CONSTRAINT agents_user_id_fkey;
ALTER TABLE agents ALTER COLUMN user_id TYPE text USING user_id::text;
ALTER TABLE agents ADD CONSTRAINT agents_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user" ("id") ON DELETE CASCADE;

DROP TABLE users;
