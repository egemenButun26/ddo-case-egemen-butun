-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE lots (
  id bigserial PRIMARY KEY,
  code text NOT NULL UNIQUE CHECK (code <> ''),
  name text NOT NULL CHECK (name <> ''),
  capacity integer NOT NULL CHECK (capacity > 0),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tariff_versions (
  id text PRIMARY KEY CHECK (id <> ''),
  lot_id bigint NOT NULL REFERENCES lots(id),
  valid_from timestamptz NOT NULL,
  valid_to timestamptz,
  definition jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (valid_to IS NULL OR valid_to > valid_from),
  CONSTRAINT tariff_versions_no_overlap EXCLUDE USING gist (lot_id WITH =, tstzrange(valid_from, valid_to, '[)') WITH &&)
);

CREATE TABLE plate_events (
  id bigserial PRIMARY KEY,
  lot_id bigint NOT NULL REFERENCES lots(id),
  camera_id text NOT NULL CHECK (camera_id <> ''),
  camera_event_id text NOT NULL CHECK (camera_event_id <> ''),
  direction text NOT NULL CHECK (direction IN ('entry','exit')),
  plate_raw text NOT NULL,
  plate_norm text NOT NULL,
  captured_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  outcome text NOT NULL CHECK (outcome IN ('opened','duplicate_entry','closed','duplicate_exit','orphan_exit','review')),
  session_id bigint,
  CONSTRAINT plate_events_camera_event_uniq UNIQUE (camera_id, camera_event_id)
);

CREATE TABLE sessions (
  id bigserial PRIMARY KEY,
  lot_id bigint NOT NULL REFERENCES lots(id),
  plate text NOT NULL CHECK (plate <> ''),
  status text NOT NULL CHECK (status IN ('open','closed')),
  entry_event_id bigint NOT NULL UNIQUE REFERENCES plate_events(id),
  entered_at timestamptz NOT NULL,
  exit_event_id bigint UNIQUE REFERENCES plate_events(id),
  exited_at timestamptz,
  tariff_version_id text NOT NULL REFERENCES tariff_versions(id),
  amount_kurus bigint CHECK (amount_kurus >= 0),
  quote jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sessions_state_chk CHECK (
    (status = 'open' AND exited_at IS NULL AND exit_event_id IS NULL AND amount_kurus IS NULL)
    OR (status = 'closed' AND exited_at IS NOT NULL AND exit_event_id IS NOT NULL AND amount_kurus IS NOT NULL AND exited_at >= entered_at))
);

CREATE UNIQUE INDEX sessions_one_open_per_plate ON sessions (lot_id, plate) WHERE status = 'open';
CREATE INDEX sessions_closed_recent ON sessions (lot_id, exited_at) WHERE status = 'closed';

ALTER TABLE plate_events ADD CONSTRAINT plate_events_session_fk FOREIGN KEY (session_id) REFERENCES sessions(id);
CREATE INDEX plate_events_orphan_exits ON plate_events (lot_id, captured_at) WHERE outcome = 'orphan_exit';

CREATE TABLE review_items (
  id bigserial PRIMARY KEY,
  event_id bigint NOT NULL UNIQUE REFERENCES plate_events(id),
  lot_id bigint NOT NULL REFERENCES lots(id),
  reason text NOT NULL CHECK (reason <> ''),
  read_plate text NOT NULL,
  suggested_session_id bigint REFERENCES sessions(id),
  confidence integer CHECK (confidence BETWEEN 0 AND 1000),
  runner_up_session_id bigint REFERENCES sessions(id),
  runner_up_confidence integer CHECK (runner_up_confidence BETWEEN 0 AND 1000),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  CHECK ((status = 'open' AND resolved_at IS NULL) OR (status = 'resolved' AND resolved_at IS NOT NULL))
);

CREATE INDEX review_items_open ON review_items (lot_id, created_at) WHERE status = 'open';

-- +goose Down
DROP TABLE review_items;
ALTER TABLE plate_events DROP CONSTRAINT plate_events_session_fk;
DROP TABLE sessions;
DROP TABLE plate_events;
DROP TABLE tariff_versions;
DROP TABLE lots;
