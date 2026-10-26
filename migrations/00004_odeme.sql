-- +goose Up
ALTER TABLE sessions ADD CONSTRAINT sessions_id_lot_plate_uniq UNIQUE (id, lot_id, plate);

CREATE TABLE payments (
  id bigserial PRIMARY KEY,
  session_id bigint NOT NULL REFERENCES sessions(id),
  amount_kurus bigint NOT NULL CHECK (amount_kurus > 0),
  status text NOT NULL CHECK (status IN ('pending','succeeded','failed','timed_out')),
  provider_ref text NOT NULL CHECK (provider_ref <> ''),
  idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
  request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
  late_approval boolean NOT NULL DEFAULT false,
  requested_at timestamptz NOT NULL,
  paid_at timestamptz,
  confirmed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT payments_provider_ref_uniq UNIQUE (provider_ref),
  CONSTRAINT payments_idempotency_key_uniq UNIQUE (idempotency_key),
  CONSTRAINT payments_state_chk CHECK (
    (status = 'succeeded' AND paid_at IS NOT NULL AND confirmed_at IS NOT NULL
      AND requested_at <= paid_at AND paid_at <= confirmed_at)
    OR (status <> 'succeeded' AND paid_at IS NULL AND confirmed_at IS NULL)
  ),
  CONSTRAINT payments_late_approval_chk CHECK (NOT late_approval OR status = 'succeeded')
);

CREATE INDEX payments_session_idx ON payments (session_id, requested_at);
CREATE UNIQUE INDEX payments_one_pending_per_session ON payments (session_id) WHERE status = 'pending';
CREATE INDEX payments_pending_requested_idx ON payments (requested_at) WHERE status = 'pending';

CREATE TABLE refunds (
  id bigserial PRIMARY KEY,
  payment_id bigint NOT NULL REFERENCES payments(id),
  amount_kurus bigint NOT NULL CHECK (amount_kurus > 0),
  status text NOT NULL CHECK (status IN ('pending','succeeded','failed')),
  reason text NOT NULL CHECK (reason IN ('overpaid','manual')),
  idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
  request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
  provider_ref text CHECK (provider_ref IS NULL OR provider_ref <> ''),
  requested_at timestamptz NOT NULL,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT refunds_idempotency_key_uniq UNIQUE (idempotency_key),
  CONSTRAINT refunds_provider_ref_uniq UNIQUE (provider_ref),
  CONSTRAINT refunds_state_chk CHECK (
    (status = 'pending' AND completed_at IS NULL)
    OR (status IN ('succeeded','failed') AND completed_at IS NOT NULL AND completed_at >= requested_at)
  ),
  CONSTRAINT refunds_succeeded_ref_chk CHECK (status <> 'succeeded' OR provider_ref IS NOT NULL)
);

CREATE INDEX refunds_payment_idx ON refunds (payment_id);

-- "iade toplami <= odeme tutari" ve "yalniz basarili odeme iade edilir"
-- satirlar arasi kurallardir; CHECK ile ifade edilemez, store katmani
-- transaction + kilitle saglayacak.

CREATE TABLE debts (
  id bigserial PRIMARY KEY,
  session_id bigint NOT NULL,
  lot_id bigint NOT NULL,
  plate text NOT NULL CHECK (plate <> ''),
  amount_kurus bigint NOT NULL CHECK (amount_kurus > 0),
  paid_kurus bigint NOT NULL DEFAULT 0 CHECK (paid_kurus >= 0),
  remaining_kurus bigint GENERATED ALWAYS AS (amount_kurus - paid_kurus) STORED,
  status text NOT NULL CHECK (status IN ('open','settled','written_off')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  closed_at timestamptz,
  CONSTRAINT debts_session_uniq UNIQUE (session_id),
  CONSTRAINT debts_session_fk FOREIGN KEY (session_id, lot_id, plate) REFERENCES sessions (id, lot_id, plate),
  CONSTRAINT debts_paid_le_amount_chk CHECK (paid_kurus <= amount_kurus),
  CONSTRAINT debts_state_chk CHECK (
    (status = 'open' AND paid_kurus < amount_kurus AND closed_at IS NULL)
    OR (status = 'settled' AND paid_kurus = amount_kurus AND closed_at IS NOT NULL)
    OR (status = 'written_off' AND paid_kurus < amount_kurus AND closed_at IS NOT NULL)
  )
);

CREATE INDEX debts_open_plate_idx ON debts (plate) WHERE status = 'open';
CREATE INDEX debts_lot_created_idx ON debts (lot_id, created_at);

-- +goose Down
DROP TABLE debts;
DROP TABLE refunds;
DROP TABLE payments;
ALTER TABLE sessions DROP CONSTRAINT sessions_id_lot_plate_uniq;
