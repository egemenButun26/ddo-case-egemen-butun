package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
)

var (
	ErrPlanNotFound          = errors.New("plan bulunamadi")
	ErrPlanExists            = errors.New("plan kodu zaten var")
	ErrSubscriptionNotFound  = errors.New("abonelik bulunamadi")
	ErrSubscriptionExists    = errors.New("plaka icin zaten aktif abonelik var")
	ErrSubscriptionCancelled = errors.New("abonelik iptal edilmis")
	ErrInvoiceNotFound       = errors.New("fatura bulunamadi")
	ErrInvoiceAlreadyPaid    = errors.New("fatura zaten odendi")
)

type Plan struct {
	ID           int64
	Code         string
	LotID        int64
	MonthlyPrice tariff.Kurus
	Active       bool
}

type Subscription struct {
	ID                 int64
	PlanID             int64
	PlanCode           string
	MonthlyPrice       tariff.Kurus
	LotID              int64
	Plate              string
	UserID             *int64
	Status             billing.Status
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	CancelledAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func dateText(t time.Time) string {
	return t.Format("2006-01-02")
}

func isConstraint(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == name
}

func InsertPlan(ctx context.Context, q Querier, code string, lotID int64, price tariff.Kurus) (int64, error) {
	if code == "" {
		return 0, ErrPlanNotFound
	}
	if price <= 0 {
		return 0, billing.ErrNegativeAmount
	}
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO plans (code, lot_id, monthly_price_kurus) VALUES ($1, $2, $3) RETURNING id`,
		code, lotID, int64(price),
	).Scan(&id)
	if isConstraint(err, "plans_code_uniq") {
		return 0, ErrPlanExists
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func GetPlanByCode(ctx context.Context, q Querier, code string) (Plan, error) {
	var p Plan
	var price int64
	err := q.QueryRow(ctx,
		`SELECT id, code, lot_id, monthly_price_kurus, active FROM plans WHERE code = $1`, code,
	).Scan(&p.ID, &p.Code, &p.LotID, &price, &p.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, err
	}
	p.MonthlyPrice = tariff.Kurus(price)
	return p, nil
}

const subscriptionSelect = `SELECT s.id, s.plan_id, p.code, p.monthly_price_kurus, p.lot_id,
       s.plate, s.user_id, s.status, s.current_period_start, s.current_period_end,
       s.cancelled_at, s.created_at, s.updated_at
  FROM subscriptions s
  JOIN plans p ON p.id = s.plan_id`

func scanSubscription(row pgx.Row) (Subscription, error) {
	var s Subscription
	var price int64
	var status string
	err := row.Scan(&s.ID, &s.PlanID, &s.PlanCode, &price, &s.LotID, &s.Plate,
		&s.UserID, &status, &s.CurrentPeriodStart, &s.CurrentPeriodEnd,
		&s.CancelledAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrSubscriptionNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	s.MonthlyPrice = tariff.Kurus(price)
	s.Status = billing.Status(status)
	return s, nil
}

func GetSubscription(ctx context.Context, q Querier, id int64) (Subscription, error) {
	return scanSubscription(q.QueryRow(ctx, subscriptionSelect+` WHERE s.id = $1`, id))
}

func LockSubscription(ctx context.Context, q Querier, id int64) (Subscription, error) {
	return scanSubscription(q.QueryRow(ctx, subscriptionSelect+` WHERE s.id = $1 FOR UPDATE OF s`, id))
}

func InsertSubscription(ctx context.Context, q Querier, planID int64, plate string, periodStart, periodEnd time.Time) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO subscriptions (plan_id, plate, status, current_period_start, current_period_end)
		 VALUES ($1, $2, 'active', $3, $4) RETURNING id`,
		planID, plate, dateText(periodStart), dateText(periodEnd),
	).Scan(&id)
	if isConstraint(err, "subscriptions_one_active_per_plate_lot") {
		return 0, ErrSubscriptionExists
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func SetSubscriptionStatus(ctx context.Context, q Querier, id int64, status billing.Status, cancelledAt *time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE subscriptions SET status = $2, cancelled_at = $3, updated_at = now() WHERE id = $1`,
		id, string(status), cancelledAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}

func SetSubscriptionPlan(ctx context.Context, q Querier, id, planID int64) error {
	tag, err := q.Exec(ctx,
		`UPDATE subscriptions SET plan_id = $2, updated_at = now() WHERE id = $1`, id, planID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}

func SetSubscriptionPeriod(ctx context.Context, q Querier, id int64, periodStart, periodEnd time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE subscriptions SET current_period_start = $2, current_period_end = $3, updated_at = now()
		 WHERE id = $1`,
		id, dateText(periodStart), dateText(periodEnd),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSubscriptionNotFound
	}
	return nil
}

func ActiveSubscriptionsForPeriod(ctx context.Context, q Querier, periodStart time.Time) ([]Subscription, error) {
	rows, err := q.Query(ctx,
		subscriptionSelect+` WHERE s.status = 'active' AND s.current_period_start = $1
		 ORDER BY s.id FOR UPDATE OF s`,
		dateText(periodStart),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return subs, nil
}
