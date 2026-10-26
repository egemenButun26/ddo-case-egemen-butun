package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
)

const (
	InvoiceKindPeriod          = "period"
	InvoiceKindProrationCharge = "proration_charge"
	InvoiceKindProrationCredit = "proration_credit"
)

const (
	InvoiceStatusOpen   = "open"
	InvoiceStatusPaid   = "paid"
	InvoiceStatusFailed = "failed"
	InvoiceStatusVoid   = "void"
)

type Invoice struct {
	ID             int64
	SubscriptionID int64
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Kind           string
	Amount         tariff.Kurus
	Status         string
	Attempts       int
	NextRetryAt    *time.Time
	PaidAt         *time.Time
	CreatedAt      time.Time
}

const invoiceSelect = `SELECT i.id, i.subscription_id, i.period_start, i.period_end, i.kind,
       i.amount_kurus, i.status, i.attempts, i.next_retry_at, i.paid_at, i.created_at
  FROM invoices i`

func scanInvoice(row pgx.Row) (Invoice, error) {
	var inv Invoice
	var amount int64
	err := row.Scan(&inv.ID, &inv.SubscriptionID, &inv.PeriodStart, &inv.PeriodEnd, &inv.Kind,
		&amount, &inv.Status, &inv.Attempts, &inv.NextRetryAt, &inv.PaidAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrInvoiceNotFound
	}
	if err != nil {
		return Invoice{}, err
	}
	inv.Amount = tariff.Kurus(amount)
	return inv, nil
}

func GetInvoice(ctx context.Context, q Querier, id int64) (Invoice, error) {
	return scanInvoice(q.QueryRow(ctx, invoiceSelect+` WHERE i.id = $1`, id))
}

func InsertInvoice(ctx context.Context, q Querier, inv Invoice) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO invoices (subscription_id, period_start, period_end, kind, amount_kurus,
		                       status, attempts, next_retry_at, paid_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		inv.SubscriptionID, dateText(inv.PeriodStart), dateText(inv.PeriodEnd), inv.Kind,
		int64(inv.Amount), inv.Status, inv.Attempts, inv.NextRetryAt, inv.PaidAt,
	).Scan(&id)
	return id, err
}

func InsertInvoiceIfAbsent(ctx context.Context, q Querier, inv Invoice) (bool, error) {
	tag, err := q.Exec(ctx,
		`INSERT INTO invoices (subscription_id, period_start, period_end, kind, amount_kurus,
		                       status, attempts, next_retry_at, paid_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (subscription_id, period_start, kind) DO NOTHING`,
		inv.SubscriptionID, dateText(inv.PeriodStart), dateText(inv.PeriodEnd), inv.Kind,
		int64(inv.Amount), inv.Status, inv.Attempts, inv.NextRetryAt, inv.PaidAt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func LockInvoiceWithSubscription(ctx context.Context, q Querier, id int64) (Invoice, billing.Status, error) {
	row := q.QueryRow(ctx,
		`SELECT i.id, i.subscription_id, i.period_start, i.period_end, i.kind,
		        i.amount_kurus, i.status, i.attempts, i.next_retry_at, i.paid_at, i.created_at,
		        s.status
		   FROM invoices i
		   JOIN subscriptions s ON s.id = i.subscription_id
		  WHERE i.id = $1
		  FOR UPDATE OF i, s`, id)
	var inv Invoice
	var amount int64
	var subStatus string
	err := row.Scan(&inv.ID, &inv.SubscriptionID, &inv.PeriodStart, &inv.PeriodEnd, &inv.Kind,
		&amount, &inv.Status, &inv.Attempts, &inv.NextRetryAt, &inv.PaidAt, &inv.CreatedAt,
		&subStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, "", ErrInvoiceNotFound
	}
	if err != nil {
		return Invoice{}, "", err
	}
	inv.Amount = tariff.Kurus(amount)
	return inv, billing.Status(subStatus), nil
}

func MarkInvoicePaid(ctx context.Context, q Querier, id int64, at time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE invoices SET status = 'paid', paid_at = $2, attempts = 0, next_retry_at = NULL
		 WHERE id = $1`, id, at,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvoiceNotFound
	}
	return nil
}

func MarkInvoiceFailed(ctx context.Context, q Querier, id int64, attempts int, nextRetry *time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE invoices SET status = 'failed', attempts = $2, next_retry_at = $3 WHERE id = $1`,
		id, attempts, nextRetry,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvoiceNotFound
	}
	return nil
}

func DueRetryInvoices(ctx context.Context, q Querier, now time.Time) ([]Invoice, error) {
	rows, err := q.Query(ctx,
		invoiceSelect+`
		 JOIN subscriptions s ON s.id = i.subscription_id
		 WHERE i.status IN ('open','failed') AND i.next_retry_at IS NOT NULL
		   AND i.next_retry_at <= $1 AND s.status IN ('active','past_due')
		 ORDER BY i.next_retry_at, i.id
		 FOR UPDATE OF i SKIP LOCKED`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invoices := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invoices, nil
}

func PaidInvoicesOnDay(ctx context.Context, q Querier, start, end time.Time) ([]Invoice, error) {
	rows, err := q.Query(ctx,
		invoiceSelect+` WHERE i.status = 'paid' AND i.paid_at >= $1 AND i.paid_at < $2 ORDER BY i.id`,
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invoices := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invoices, nil
}

func StartBillingRun(ctx context.Context, q Querier, periodStart time.Time) (int64, bool, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO billing_runs (period_start) VALUES ($1)
		 ON CONFLICT (period_start) DO NOTHING RETURNING id`,
		dateText(periodStart),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func FinishBillingRun(ctx context.Context, q Querier, id int64, invoicesCreated int) error {
	_, err := q.Exec(ctx,
		`UPDATE billing_runs SET finished_at = now(), invoices_created = $2 WHERE id = $1`,
		id, invoicesCreated,
	)
	return err
}
