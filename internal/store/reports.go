package store

import (
	"context"
	"time"
)

// DailyReport, bir yerel gun icin ozet sayilardir.
type DailyReport struct {
	Entries          int
	Exits            int
	ClosedSessions   int
	TotalAmountKurus int64
	CollectedKurus   int64
	OpenDebtKurus    int64
	OpenReviews      int
}

// GetDailyReport, [start, end) araligindaki gunluk raporu hesaplar.
// lotID sifirsa tum otoparklar dahil edilir.
func GetDailyReport(ctx context.Context, q Querier, lotID int64, start, end time.Time) (DailyReport, error) {
	var r DailyReport
	err := q.QueryRow(ctx,
		`SELECT
		   (SELECT count(*) FROM plate_events
		     WHERE direction = 'entry' AND captured_at >= $2 AND captured_at < $3
		       AND ($1 = 0 OR lot_id = $1)),
		   (SELECT count(*) FROM plate_events
		     WHERE direction = 'exit' AND captured_at >= $2 AND captured_at < $3
		       AND ($1 = 0 OR lot_id = $1)),
		   (SELECT count(*) FROM sessions
		     WHERE status = 'closed' AND exited_at >= $2 AND exited_at < $3
		       AND ($1 = 0 OR lot_id = $1)),
		   (SELECT COALESCE(sum(amount_kurus), 0) FROM sessions
		     WHERE status = 'closed' AND exited_at >= $2 AND exited_at < $3
		       AND ($1 = 0 OR lot_id = $1)),
		   (SELECT COALESCE(sum(p.amount_kurus), 0) FROM payments p
		     JOIN sessions s ON s.id = p.session_id
		     WHERE p.status = 'succeeded' AND p.paid_at >= $2 AND p.paid_at < $3
		       AND ($1 = 0 OR s.lot_id = $1)),
		   (SELECT COALESCE(sum(remaining_kurus), 0) FROM debts
		     WHERE status = 'open' AND ($1 = 0 OR lot_id = $1)),
		   (SELECT count(*) FROM review_items
		     WHERE status = 'open' AND created_at >= $2 AND created_at < $3
		       AND ($1 = 0 OR lot_id = $1))`,
		lotID, start, end,
	).Scan(&r.Entries, &r.Exits, &r.ClosedSessions, &r.TotalAmountKurus,
		&r.CollectedKurus, &r.OpenDebtKurus, &r.OpenReviews)
	if err != nil {
		return DailyReport{}, err
	}
	return r, nil
}
