package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/tariff"
)

// StoredDebt, veritabanindaki borc satiridir.
type StoredDebt struct {
	ID        int64
	SessionID int64
	LotID     int64
	Plate     string
	Amount    tariff.Kurus
	Paid      tariff.Kurus
	Remaining tariff.Kurus
	Status    string
	ClosedAt  *time.Time
}

// InsertDebt acik borc kaydi olusturur ve kimligini doner.
func InsertDebt(ctx context.Context, q Querier, sessionID, lotID int64, plateRaw string, amount tariff.Kurus) (int64, error) {
	if sessionID <= 0 || lotID <= 0 {
		return 0, errors.New("borc: oturum ya da otopark kimligi gecersiz")
	}
	p := plate.Normalize(plateRaw)
	if p == "" {
		return 0, errors.New("borc: plaka bos")
	}
	if amount <= 0 {
		return 0, fmt.Errorf("%w: borc tutari pozitif olmali (%d)", tariff.ErrNegativeAmount, amount)
	}

	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO "ya-debts" (session_id, lot_id, plate, amount_kurus, paid_kurus, status)
		 VALUES ($1, $2, $3, $4, 0, 'open')
		 RETURNING id`,
		sessionID, lotID, p, amount,
	).Scan(&id)
	if err != nil {
		return 0, mapPaymentPgError(err)
	}
	return id, nil
}

// SumOpenDebtByPlate plakanin acik borc kalanlarinin toplamini doner.
func SumOpenDebtByPlate(ctx context.Context, q Querier, plateRaw string) (tariff.Kurus, error) {
	var toplam tariff.Kurus
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(remaining_kurus), 0) FROM "ya-debts"
		 WHERE plate = $1 AND status = 'open'`, plate.Normalize(plateRaw),
	).Scan(&toplam); err != nil {
		return 0, err
	}
	return toplam, nil
}

// SumOpenDebtByPlateExcept plakanin acik borc kalanlarinin toplamini, verilen
// oturumun kendi borc kaydi haric, doner.
func SumOpenDebtByPlateExcept(ctx context.Context, q Querier, plateRaw string, sessionID int64) (tariff.Kurus, error) {
	var toplam tariff.Kurus
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(sum(remaining_kurus), 0) FROM "ya-debts"
		 WHERE plate = $1 AND status = 'open' AND session_id <> $2`,
		plate.Normalize(plateRaw), sessionID,
	).Scan(&toplam); err != nil {
		return 0, err
	}
	return toplam, nil
}

// ListOpenDebtsByPlate plakanin acik borclarini kimlik sirasiyla doner.
func ListOpenDebtsByPlate(ctx context.Context, q Querier, plateRaw string) ([]StoredDebt, error) {
	rows, err := q.Query(ctx,
		`SELECT id, session_id, lot_id, plate, amount_kurus, paid_kurus, remaining_kurus,
		        status, closed_at
		 FROM "ya-debts" WHERE plate = $1 AND status = 'open' ORDER BY id`, plate.Normalize(plateRaw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredDebt
	for rows.Next() {
		var (
			d        StoredDebt
			closedAt *time.Time
		)
		if err := rows.Scan(&d.ID, &d.SessionID, &d.LotID, &d.Plate, &d.Amount, &d.Paid,
			&d.Remaining, &d.Status, &closedAt); err != nil {
			return nil, err
		}
		if closedAt != nil {
			t := closedAt.UTC()
			d.ClosedAt = &t
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
