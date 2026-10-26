package store

import (
	"context"
	"errors"
	"strconv"
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

var ErrProrationInvoiceClosed = errors.New("proration faturasi kapanmis")

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

// AddProrationInvoice, ayni abonelik/donem/kind icin var olan proration faturasinin
// tutarina yeni tutari ekler; yoksa olusturur. UNIQUE kisiti migration degismeden
// korunur ve art arda plan gecisi/iptal ayni donemde 23505 uretmez.
// Yalnizca open/failed fatura birlesir; paid/void faturaya dokunulmaz ve
// ErrProrationInvoiceClosed doner.
func AddProrationInvoice(ctx context.Context, q Querier, inv Invoice) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO invoices (subscription_id, period_start, period_end, kind, amount_kurus,
		                       status, attempts, next_retry_at, paid_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (subscription_id, period_start, kind) DO UPDATE
		   SET amount_kurus = invoices.amount_kurus + EXCLUDED.amount_kurus,
		       period_end = EXCLUDED.period_end
		   WHERE invoices.status IN ('open','failed')
		 RETURNING id`,
		inv.SubscriptionID, dateText(inv.PeriodStart), dateText(inv.PeriodEnd), inv.Kind,
		int64(inv.Amount), inv.Status, inv.Attempts, inv.NextRetryAt, inv.PaidAt,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrProrationInvoiceClosed
	}
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

type InvoiceFilter struct {
	SubscriptionID *int64
	Status         string
	Plate          *string
	OwnerUserID    *int64
	AfterID        int64
	Limit          int
}

func ListInvoices(ctx context.Context, q Querier, f InvoiceFilter) ([]Invoice, error) {
	query := invoiceSelect + ` JOIN subscriptions s ON s.id = i.subscription_id WHERE TRUE`
	args := []any{}
	if f.SubscriptionID != nil {
		args = append(args, *f.SubscriptionID)
		query += ` AND i.subscription_id = $` + strconv.Itoa(len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		query += ` AND i.status = $` + strconv.Itoa(len(args))
	}
	if f.Plate != nil && f.OwnerUserID != nil {
		args = append(args, *f.OwnerUserID)
		ownerArg := strconv.Itoa(len(args))
		args = append(args, *f.Plate)
		plateArg := strconv.Itoa(len(args))
		query += ` AND (s.user_id = $` + ownerArg + ` OR s.plate = $` + plateArg + `)`
	} else if f.Plate != nil {
		args = append(args, *f.Plate)
		query += ` AND s.plate = $` + strconv.Itoa(len(args))
	} else if f.OwnerUserID != nil {
		args = append(args, *f.OwnerUserID)
		query += ` AND s.user_id = $` + strconv.Itoa(len(args))
	}
	if f.AfterID > 0 {
		args = append(args, f.AfterID)
		query += ` AND i.id > $` + strconv.Itoa(len(args))
	}
	args = append(args, f.Limit)
	query += ` ORDER BY i.id LIMIT $` + strconv.Itoa(len(args))

	rows, err := q.Query(ctx, query, args...)
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
