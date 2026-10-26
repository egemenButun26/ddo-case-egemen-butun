package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/session"
)

// ErrReviewNotOpen: inceleme yok ya da zaten cozulmus.
var ErrReviewNotOpen = errors.New("inceleme acik degil")

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

// ReviewListItem, inceleme kuyrugu satiridir.
type ReviewListItem struct {
	ID                 int64
	EventID            int64
	LotID              int64
	Reason             string
	ReadPlate          string
	Status             string
	CreatedAt          time.Time
	ResolvedAt         *time.Time
	SuggestedSessionID *int64
	PlateRaw           string
	PlateNorm          string
	CapturedAt         time.Time
}

// ListReviews, durum filtresiyle incelemeleri kimlik sirasiyla doner.
func ListReviews(ctx context.Context, q Querier, status string, afterID int64, limit int) ([]ReviewListItem, error) {
	rows, err := q.Query(ctx,
		`SELECT ri.id, ri.event_id, ri.lot_id, ri.reason, ri.read_plate, ri.status,
		        ri.created_at, ri.resolved_at, ri.suggested_session_id,
		        pe.plate_raw, pe.plate_norm, pe.captured_at
		 FROM review_items ri
		 JOIN plate_events pe ON pe.id = ri.event_id
		 WHERE ($1 = '' OR ri.status = $1) AND ri.id > $2
		 ORDER BY ri.id LIMIT $3`,
		status, afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReviewListItem
	for rows.Next() {
		var (
			item     ReviewListItem
			resolved *time.Time
		)
		if err := rows.Scan(&item.ID, &item.EventID, &item.LotID, &item.Reason, &item.ReadPlate,
			&item.Status, &item.CreatedAt, &resolved, &item.SuggestedSessionID,
			&item.PlateRaw, &item.PlateNorm, &item.CapturedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.CapturedAt = item.CapturedAt.UTC()
		if resolved != nil {
			u := resolved.UTC()
			item.ResolvedAt = &u
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReviewRecord, cozumleme icin gerekli inceleme alanlaridir.
type ReviewRecord struct {
	ID                 int64
	EventID            int64
	LotID              int64
	Status             string
	SuggestedSessionID *int64
}

// GetReview, kimligiyle incelemeyi doner.
func GetReview(ctx context.Context, q Querier, id int64) (ReviewRecord, error) {
	var r ReviewRecord
	err := q.QueryRow(ctx,
		`SELECT id, event_id, lot_id, status, suggested_session_id
		 FROM review_items WHERE id = $1`,
		id,
	).Scan(&r.ID, &r.EventID, &r.LotID, &r.Status, &r.SuggestedSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewRecord{}, ErrNotFound
	}
	if err != nil {
		return ReviewRecord{}, err
	}
	return r, nil
}

// ResolveReview, acik incelemeyi cozulmus yapar.
func ResolveReview(ctx context.Context, q Querier, id int64, at time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE review_items SET status = 'resolved', resolved_at = $2
		 WHERE id = $1 AND status = 'open'`,
		id, at,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var status string
	err = q.QueryRow(ctx, `SELECT status FROM review_items WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrReviewNotOpen
}

// LinkReviewEvent, inceleme olayini verilen oturuma baglar.
func LinkReviewEvent(ctx context.Context, q Querier, eventID, sessionID int64) error {
	tag, err := q.Exec(ctx,
		`UPDATE plate_events SET session_id = $2 WHERE id = $1`,
		eventID, sessionID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
