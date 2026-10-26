-- +goose Up
CREATE TABLE plans (
  id bigserial PRIMARY KEY,
  code text NOT NULL CHECK (code <> ''),
  lot_id bigint NOT NULL REFERENCES lots(id),
  monthly_price_kurus bigint NOT NULL CHECK (monthly_price_kurus > 0),
  active boolean NOT NULL DEFAULT true,
  CONSTRAINT plans_code_uniq UNIQUE (code)
);

CREATE TABLE subscriptions (
  id bigserial PRIMARY KEY,
  plan_id bigint NOT NULL REFERENCES plans(id),
  plate text NOT NULL CHECK (plate <> ''),
  -- user_id iliskisi bilerek yok: users tablosu paralel dalda geliyor.
  user_id bigint NULL,
  status text NOT NULL CHECK (status IN ('active','past_due','suspended','cancelled')),
  current_period_start date NOT NULL,
  current_period_end date NOT NULL,
  cancelled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT subscriptions_period_chk CHECK (current_period_end > current_period_start)
);

CREATE UNIQUE INDEX subscriptions_one_active_per_plate_lot
  ON subscriptions (plate, plan_id)
  WHERE status IN ('active','past_due');

CREATE TABLE invoices (
  id bigserial PRIMARY KEY,
  subscription_id bigint NOT NULL REFERENCES subscriptions(id),
  period_start date NOT NULL,
  period_end date NOT NULL,
  kind text NOT NULL CHECK (kind IN ('period','proration_charge','proration_credit')),
  amount_kurus bigint NOT NULL CHECK (amount_kurus >= 0),
  status text NOT NULL CHECK (status IN ('open','paid','failed','void')),
  attempts int NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_retry_at timestamptz,
  paid_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT invoices_period_chk CHECK (period_end > period_start),
  CONSTRAINT invoices_paid_chk CHECK ((status = 'paid') = (paid_at IS NOT NULL)),
  CONSTRAINT invoices_subscription_period_kind_uniq UNIQUE (subscription_id, period_start, kind)
);

CREATE INDEX invoices_retry_idx
  ON invoices (next_retry_at)
  WHERE status IN ('open','failed') AND next_retry_at IS NOT NULL;

CREATE TABLE billing_runs (
  id bigserial PRIMARY KEY,
  period_start date NOT NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  invoices_created int NOT NULL DEFAULT 0 CHECK (invoices_created >= 0),
  CONSTRAINT billing_runs_period_start_uniq UNIQUE (period_start)
);

CREATE TABLE provider_settlements (
  id bigserial PRIMARY KEY,
  day date NOT NULL,
  provider_ref text NOT NULL CHECK (provider_ref <> ''),
  amount_kurus bigint NOT NULL,
  CONSTRAINT provider_settlements_day_ref_uniq UNIQUE (day, provider_ref)
);

CREATE TABLE reconciliation_reports (
  id bigserial PRIMARY KEY,
  day date NOT NULL,
  our_total bigint NOT NULL,
  provider_total bigint NOT NULL,
  diff bigint NOT NULL,
  details jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT reconciliation_reports_day_uniq UNIQUE (day)
);

-- +goose Down
DROP TABLE reconciliation_reports;
DROP TABLE provider_settlements;
DROP TABLE billing_runs;
DROP TABLE invoices;
DROP TABLE subscriptions;
DROP TABLE plans;
