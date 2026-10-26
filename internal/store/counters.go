package store

import (
	"context"
	"time"

	"parkakil/internal/domain/session"
)

// IncrementCounter artimli doluluk sayacini bir arttirir; satir yoksa olusturur.
func IncrementCounter(ctx context.Context, q Querier, lotID int64) error {
	_, err := q.Exec(ctx,
		`INSERT INTO lot_counters (lot_id, occupied, updated_at)
		 VALUES ($1, 1, now())
		 ON CONFLICT (lot_id) DO UPDATE
		 SET occupied = lot_counters.occupied + 1, updated_at = now()`,
		lotID,
	)
	return err
}

// DecrementCounter artimli doluluk sayacini bir azaltir; sifirin altina inmez.
func DecrementCounter(ctx context.Context, q Querier, lotID int64) error {
	_, err := q.Exec(ctx,
		`INSERT INTO lot_counters (lot_id, occupied, updated_at)
		 VALUES ($1, 0, now())
		 ON CONFLICT (lot_id) DO UPDATE
		 SET occupied = GREATEST(lot_counters.occupied - 1, 0), updated_at = now()`,
		lotID,
	)
	return err
}

// GetCounter saklanan doluluk sayacini doner; satir yoksa 0.
func GetCounter(ctx context.Context, q Querier, lotID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT COALESCE((SELECT occupied FROM lot_counters WHERE lot_id = $1), 0)`,
		lotID,
	).Scan(&n)
	return n, err
}

// SetCounter saklanan doluluk sayacini verilen degere esitler.
func SetCounter(ctx context.Context, q Querier, lotID int64, occupied int) error {
	_, err := q.Exec(ctx,
		`INSERT INTO lot_counters (lot_id, occupied, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (lot_id) DO UPDATE
		 SET occupied = EXCLUDED.occupied, updated_at = now()`,
		lotID, occupied,
	)
	return err
}

// InsertCounterDrift kayma kaydini ekler ve kimligini doner.
func InsertCounterDrift(ctx context.Context, q Querier, lotID int64, stored, recomputed int, detectedAt time.Time, corrected bool) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO counter_drifts (lot_id, stored, recomputed, detected_at, corrected)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		lotID, stored, recomputed, detectedAt, corrected,
	).Scan(&id)
	return id, err
}

// CountEventsByOutcome, otoparkta belirli sonuclu kamera olayi sayisini doner.
func CountEventsByOutcome(ctx context.Context, q Querier, lotID int64, outcome session.Outcome) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM plate_events WHERE lot_id = $1 AND outcome = $2`,
		lotID, string(outcome),
	).Scan(&n)
	return n, err
}

// CountOpenSessionsForLot, otoparkin acik oturum sayisini doner.
func CountOpenSessionsForLot(ctx context.Context, q Querier, lotID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE lot_id = $1 AND status = 'open'`,
		lotID,
	).Scan(&n)
	return n, err
}
