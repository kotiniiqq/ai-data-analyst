-- Application tables: users and the audit log. Not visible to the warehouse role.

CREATE TABLE users (
  id            bigserial PRIMARY KEY,
  email         text NOT NULL UNIQUE,
  password_hash text NOT NULL,
  role          text NOT NULL CHECK (role IN ('viewer', 'analyst', 'owner')),
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
  id          bigserial PRIMARY KEY,
  at          timestamptz NOT NULL DEFAULT now(),
  user_id     bigint REFERENCES users(id),
  tool        text NOT NULL,
  allowed     boolean NOT NULL,
  code        text NOT NULL DEFAULT '',
  input_keys  text[] NOT NULL DEFAULT '{}',
  duration_ms integer NOT NULL DEFAULT 0
);
CREATE INDEX audit_log_at ON audit_log (at DESC);
