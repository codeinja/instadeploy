CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             text NOT NULL,
    token_hash       text NOT NULL UNIQUE,
    status           text NOT NULL DEFAULT 'OFFLINE',
    last_seen        timestamptz,
    -- Each agent is one Pangolin "site". The agent runs Pangolin's newt
    -- client with these credentials to open the tunnel.
    pangolin_site_id integer,
    newt_id          text NOT NULL DEFAULT '',
    newt_secret      text NOT NULL DEFAULT '',
    tunnel_error     text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id             uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name                 text NOT NULL,
    image                text NOT NULL,
    container_port       integer NOT NULL,
    env                  jsonb NOT NULL DEFAULT '{}',
    container_id         text NOT NULL DEFAULT '',
    status               text NOT NULL DEFAULT 'PENDING',
    error                text NOT NULL DEFAULT '',
    public_url           text NOT NULL DEFAULT '',
    -- PENDING, READY, FAILED or DISABLED (Pangolin not configured).
    url_status           text NOT NULL DEFAULT 'PENDING',
    url_error            text NOT NULL DEFAULT '',
    pangolin_resource_id integer,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX deployments_agent_id_idx ON deployments(agent_id);
CREATE INDEX agents_user_id_idx ON agents(user_id);
