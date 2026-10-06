-- Server settings edited in the dashboard (Settings and the setup wizard),
-- so a .env file isn't needed. Values from the environment seed this table
-- the first time the server starts; after that the dashboard owns them.
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    -- Encrypted with the secrets key (base64 of the ciphertext).
    encrypted  boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
