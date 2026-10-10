-- Optional access protection for public services, enforced by Pangolin in
-- front of the app: a password or a 6-digit PIN. The secret is encrypted
-- like variables.
ALTER TABLE services
    ADD COLUMN access        text NOT NULL DEFAULT 'none'
        CHECK (access IN ('none', 'password', 'pincode')),
    ADD COLUMN access_secret bytea;

-- What protection was last applied to the route's Pangolin resource (a
-- fingerprint of the mode and secret), to know when it must be re-applied.
ALTER TABLE routes ADD COLUMN access_applied text NOT NULL DEFAULT '';
