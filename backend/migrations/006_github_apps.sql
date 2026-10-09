-- Each user can connect one GitHub App they created. Insta Deploy uses it
-- to read private repositories (short-lived installation tokens) and
-- receives its push webhooks for instant auto deploys.
CREATE TABLE github_apps (
    user_id                  text PRIMARY KEY REFERENCES "user" ("id") ON DELETE CASCADE,
    -- GitHub's numeric App ID. Webhooks name it in a header, which is how
    -- an incoming delivery finds its app (and webhook secret).
    app_id                   bigint NOT NULL UNIQUE,
    slug                     text NOT NULL,
    name                     text NOT NULL,
    owner                    text NOT NULL DEFAULT '',
    html_url                 text NOT NULL DEFAULT '',
    private_key_encrypted    bytea NOT NULL,
    webhook_secret_encrypted bytea,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);

-- ZIP uploads were removed: deployments build from Git repositories.
-- Files left in DATA_DIR/uploads can be deleted.
DROP TABLE IF EXISTS uploads;
