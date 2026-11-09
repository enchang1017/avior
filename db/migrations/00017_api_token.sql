-- +goose Up

-- FEAT-053. A credential an operator mints for something that is not a browser:
-- a script, a service, an agent through the MCP server.
--
-- Persisted and hashed, where the predecessor held its ApiAuthService tokens in a
-- process-local dictionary and lost every one of them on restart. Nothing
-- reads the token back: the only copy of the plaintext is the one shown at
-- creation, and a leaked table yields hashes of high-entropy secrets.
--
-- expires_at is NOT NULL on purpose. A token that never expires is the one that
-- is still working two years after its owner left; the predecessor expired after seven
-- days and that is the default here too.
--
-- revoked_at rather than a delete. The row is what a later investigation reads
-- to say which token a request came in on, and removing it would remove that.
CREATE TABLE api_token (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    CONSTRAINT api_token_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT api_token_expires_after_creation CHECK (expires_at > created_at)
);

-- The lookup on every authenticated call is by hash, which the UNIQUE covers.
-- This one is for the page that lists what an operator holds.
CREATE INDEX api_token_user_idx ON api_token (user_id, created_at DESC);

-- +goose Down
DROP TABLE api_token;
