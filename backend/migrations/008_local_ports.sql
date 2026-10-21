-- What each agent reports about local network access: whether its owner
-- allows publishing services on host ports (INSTA_DEPLOY_ALLOW_LOCAL_PORTS)
-- and the machine's address on its network, for links in the dashboard.
ALTER TABLE agents
    ADD COLUMN local_ports   boolean NOT NULL DEFAULT false,
    ADD COLUMN local_address text    NOT NULL DEFAULT '';
