-- +goose Up
CREATE TABLE blacklist (
  id bigserial PRIMARY KEY,
  plate text NOT NULL CHECK (plate <> ''),
  reason text NOT NULL CHECK (reason <> ''),
  active boolean NOT NULL DEFAULT true,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  removed_at timestamptz,
  CHECK ((active AND removed_at IS NULL) OR (NOT active AND removed_at IS NOT NULL))
);

CREATE UNIQUE INDEX blacklist_active_plate_uniq ON blacklist (plate) WHERE active;

-- +goose Down
DROP TABLE blacklist;
