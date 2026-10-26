package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/tariff"
)

var ErrInvalidSettlement = errors.New("gecersiz saglayici mutabakat satiri")

type SettlementRow struct {
	ProviderRef string
	Amount      tariff.Kurus
}

type PaymentTxn struct {
	ProviderRef string
	Amount      tariff.Kurus
}

func UpsertProviderSettlements(ctx context.Context, q Querier, day time.Time, rows []SettlementRow) (int, error) {
	for _, row := range rows {
		if row.ProviderRef == "" {
			return 0, ErrInvalidSettlement
		}
	}
	count := 0
	for _, row := range rows {
		if _, err := q.Exec(ctx,
			`INSERT INTO "ya-provider_settlements" (day, provider_ref, amount_kurus)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (day, provider_ref) DO UPDATE SET amount_kurus = EXCLUDED.amount_kurus`,
			dateText(day), row.ProviderRef, int64(row.Amount),
		); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func ProviderSettlementsOnDay(ctx context.Context, q Querier, day time.Time) ([]SettlementRow, error) {
	rows, err := q.Query(ctx,
		`SELECT provider_ref, amount_kurus FROM "ya-provider_settlements" WHERE day = $1 ORDER BY id`,
		dateText(day),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settlements := []SettlementRow{}
	for rows.Next() {
		var row SettlementRow
		var amount int64
		if err := rows.Scan(&row.ProviderRef, &amount); err != nil {
			return nil, err
		}
		row.Amount = tariff.Kurus(amount)
		settlements = append(settlements, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return settlements, nil
}

func SucceededPaymentsOnDay(ctx context.Context, q Querier, start, end time.Time) ([]PaymentTxn, error) {
	rows, err := q.Query(ctx,
		`SELECT provider_ref, amount_kurus FROM "ya-payments"
		 WHERE status = 'succeeded' AND paid_at >= $1 AND paid_at < $2 ORDER BY id`,
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txns := []PaymentTxn{}
	for rows.Next() {
		var txn PaymentTxn
		var amount int64
		if err := rows.Scan(&txn.ProviderRef, &amount); err != nil {
			return nil, err
		}
		txn.Amount = tariff.Kurus(amount)
		txns = append(txns, txn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return txns, nil
}

type ReconciliationRecord struct {
	OurTotal      tariff.Kurus
	ProviderTotal tariff.Kurus
	Diff          tariff.Kurus
	Details       []byte
}

func GetReconciliationReport(ctx context.Context, q Querier, day time.Time) (ReconciliationRecord, error) {
	var rec ReconciliationRecord
	var ourTotal, providerTotal, diff int64
	err := q.QueryRow(ctx,
		`SELECT our_total, provider_total, diff, details FROM "ya-reconciliation_reports" WHERE day = $1`,
		dateText(day),
	).Scan(&ourTotal, &providerTotal, &diff, &rec.Details)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReconciliationRecord{}, err
	}
	rec.OurTotal = tariff.Kurus(ourTotal)
	rec.ProviderTotal = tariff.Kurus(providerTotal)
	rec.Diff = tariff.Kurus(diff)
	return rec, nil
}

func SaveReconciliationReport(ctx context.Context, q Querier, day time.Time, ourTotal, providerTotal, diff tariff.Kurus, details []byte) error {
	_, err := q.Exec(ctx,
		`INSERT INTO "ya-reconciliation_reports" (day, our_total, provider_total, diff, details)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (day) DO UPDATE SET
		   our_total = EXCLUDED.our_total,
		   provider_total = EXCLUDED.provider_total,
		   diff = EXCLUDED.diff,
		   details = EXCLUDED.details,
		   created_at = now()`,
		dateText(day), int64(ourTotal), int64(providerTotal), int64(diff), string(details),
	)
	return err
}
