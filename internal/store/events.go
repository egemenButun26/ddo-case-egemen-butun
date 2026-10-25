package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"parkakil/internal/domain/session"
)

// StoredEvent veritabanindaki kamera olayini temsil eder.
type StoredEvent struct {
	ID            int64
	LotID         int64
	CameraID      string
	CameraEventID string
	Direction     session.Direction
	PlateRaw      string
	PlateNorm     string
	CapturedAt    time.Time
	Outcome       session.Outcome
	SessionID     int64 // 0 = NULL
}

// FindEventByCameraKey, kamera anahtariyla olayi arar.
func FindEventByCameraKey(ctx context.Context, q Querier, cameraID, cameraEventID string) (StoredEvent, bool, error) {
	var (
		ev        StoredEvent
		direction string
		outcome   string
		sessionID *int64
	)
	err := q.QueryRow(ctx,
		`SELECT id, lot_id, camera_id, camera_event_id, direction, plate_raw, plate_norm,
		        captured_at, outcome, session_id
		 FROM plate_events
		 WHERE camera_id = $1 AND camera_event_id = $2`,
		cameraID, cameraEventID,
	).Scan(&ev.ID, &ev.LotID, &ev.CameraID, &ev.CameraEventID, &direction, &ev.PlateRaw,
		&ev.PlateNorm, &ev.CapturedAt, &outcome, &sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredEvent{}, false, nil
	}
	if err != nil {
		return StoredEvent{}, false, err
	}
	ev.Direction = session.Direction(direction)
	ev.Outcome = session.Outcome(outcome)
	if sessionID != nil {
		ev.SessionID = *sessionID
	}
	ev.CapturedAt = ev.CapturedAt.UTC()
	return ev, true, nil
}

// InsertEvent kamera olayini yazar ve kimligini doner.
func InsertEvent(ctx context.Context, q Querier, ev session.Event, plateNorm string, outcome session.Outcome, sessionID int64) (int64, error) {
	var id int64
	err := q.QueryRow(ctx,
		`INSERT INTO plate_events
		   (lot_id, camera_id, camera_event_id, direction, plate_raw, plate_norm, captured_at, outcome, session_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		ev.LotID, ev.CameraID, ev.CameraEventID, string(ev.Direction), ev.PlateRaw, plateNorm,
		ev.CapturedAt, string(outcome), nullableID(sessionID),
	).Scan(&id)
	if err != nil {
		return 0, mapPgError(err)
	}
	return id, nil
}

// SetEventOutcome olayin sonucunu ve bagli oturumunu gunceller.
func SetEventOutcome(ctx context.Context, q Querier, eventID int64, outcome session.Outcome, sessionID int64) error {
	tag, err := q.Exec(ctx,
		`UPDATE plate_events SET outcome = $2, session_id = $3 WHERE id = $1`,
		eventID, string(outcome), nullableID(sessionID),
	)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListOrphanExits, since zamanindan itibaren bekleyen cikislari sirayla doner.
func ListOrphanExits(ctx context.Context, q Querier, lotID int64, since time.Time) ([]session.OrphanExit, error) {
	rows, err := q.Query(ctx,
		`SELECT id, plate_norm, captured_at
		 FROM plate_events
		 WHERE lot_id = $1 AND captured_at >= $2 AND outcome = 'orphan_exit'
		 ORDER BY captured_at, id`,
		lotID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []session.OrphanExit
	for rows.Next() {
		var o session.OrphanExit
		if err := rows.Scan(&o.EventID, &o.Plate, &o.CapturedAt); err != nil {
			return nil, err
		}
		o.CapturedAt = o.CapturedAt.UTC()
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
