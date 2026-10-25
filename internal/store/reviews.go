package store

import (
	"context"

	"parkakil/internal/domain/session"
)

// InsertReview, inceleme kaydi olusturur ve kimligini doner.
func InsertReview(ctx context.Context, q Querier, eventID, lotID int64, readPlate string, r session.Review) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO review_items
		   (event_id, lot_id, reason, read_plate, suggested_session_id, confidence,
		    runner_up_session_id, runner_up_confidence)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		eventID, lotID, r.Reason, readPlate, nullableID(r.SuggestedSessionID), r.Confidence,
		nullableID(r.RunnerUpSessionID), r.RunnerUpConfidence,
	).Scan(&id)
	if err != nil {
		return 0, mapPgError(err)
	}
	return id, nil
}
