-- +goose Up
CREATE TABLE users (
  id bigserial PRIMARY KEY,
  email text NOT NULL UNIQUE CHECK (email = lower(email) AND email <> ''),
  name text NOT NULL,
  role text NOT NULL CHECK (role IN ('driver','attendant','accountant','manager','admin')),
  password_hash text NOT NULL,
  plate text CHECK (plate IS NULL OR plate <> ''),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_sessions (
  id bigserial PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES users(id),
  token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CHECK (expires_at > created_at)
);

CREATE INDEX auth_sessions_user_idx ON auth_sessions (user_id);

CREATE TABLE audit_log (
  id bigserial PRIMARY KEY,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  actor_user_id bigint REFERENCES users(id),
  actor_kind text NOT NULL CHECK (actor_kind IN ('user','camera','system','provider')),
  action text NOT NULL CHECK (action <> ''),
  entity text NOT NULL,
  entity_id text NOT NULL,
  old_value jsonb,
  new_value jsonb,
  request_id text
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id, occurred_at);

-- +goose StatementBegin
CREATE FUNCTION audit_log_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_log append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_append_only
BEFORE UPDATE OR DELETE ON audit_log
FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

-- +goose Down
DROP TABLE audit_log;
DROP FUNCTION audit_log_append_only();
DROP TABLE auth_sessions;
DROP TABLE users;
