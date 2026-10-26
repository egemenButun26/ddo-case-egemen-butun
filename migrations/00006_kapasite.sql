-- +goose Up
CREATE TABLE reservations (
  id bigserial PRIMARY KEY,
  lot_id bigint NOT NULL REFERENCES lots(id),
  plate text NOT NULL CHECK (plate <> ''),
  status text NOT NULL CHECK (status IN ('active','arrived','released','cancelled')),
  reserved_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  arrived_at timestamptz,
  released_at timestamptz,
  session_id bigint REFERENCES sessions(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT reservations_expiry_chk CHECK (expires_at = reserved_at + interval '30 minutes'),
  CONSTRAINT reservations_state_chk CHECK (
    (status = 'active' AND arrived_at IS NULL AND released_at IS NULL)
    OR (status = 'arrived' AND arrived_at IS NOT NULL AND session_id IS NOT NULL AND released_at IS NULL)
    OR (status IN ('released','cancelled') AND released_at IS NOT NULL)
  )
);

CREATE UNIQUE INDEX reservations_one_active_per_plate
  ON reservations (lot_id, plate) WHERE status = 'active';
CREATE INDEX reservations_active_expiry
  ON reservations (expires_at) WHERE status = 'active';

CREATE TABLE lot_counters (
  lot_id bigint PRIMARY KEY REFERENCES lots(id),
  occupied integer NOT NULL CHECK (occupied >= 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE counter_drifts (
  id bigserial PRIMARY KEY,
  lot_id bigint NOT NULL REFERENCES lots(id),
  stored integer NOT NULL,
  recomputed integer NOT NULL,
  detected_at timestamptz NOT NULL,
  corrected boolean NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION counter_drifts_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'counter_drifts salt-ekleme tablosudur';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER counter_drifts_append_only_trg
  BEFORE UPDATE OR DELETE ON counter_drifts
  FOR EACH ROW EXECUTE FUNCTION counter_drifts_append_only();

INSERT INTO lot_counters (lot_id, occupied)
SELECT l.id, count(s.id)
FROM lots l
LEFT JOIN sessions s ON s.lot_id = l.id AND s.status = 'open'
GROUP BY l.id;

-- +goose Down
DROP TRIGGER counter_drifts_append_only_trg ON counter_drifts;
DROP FUNCTION counter_drifts_append_only();
DROP TABLE counter_drifts;
DROP TABLE lot_counters;
DROP TABLE reservations;
