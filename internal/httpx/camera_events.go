package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/sessionsvc"
)

// EventHandler, kamera olayini isleyen servis katmanini temsil eder.
type EventHandler interface {
	HandleEvent(ctx context.Context, ev session.Event) (sessionsvc.Result, error)
}

const maxEventBodyBytes = 64 << 10

type cameraEventRequest struct {
	LotID         int64     `json:"lot_id"`
	CameraID      string    `json:"camera_id"`
	CameraEventID string    `json:"camera_event_id"`
	Direction     string    `json:"direction"`
	PlateRaw      string    `json:"plate_raw"`
	CapturedAt    time.Time `json:"captured_at"`
}

type cameraEventResponse struct {
	EventID            int64           `json:"event_id"`
	Outcome            session.Outcome `json:"outcome"`
	SessionID          *int64          `json:"session_id"`
	ReviewID           *int64          `json:"review_id"`
	AmountKurus        *tariff.Kurus   `json:"amount_kurus"`
	Replayed           bool            `json:"replayed"`
	ClosedByOrphanExit bool            `json:"closed_by_orphan_exit"`
}

func cameraEventHandler(events EventHandler, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxEventBodyBytes)

		var req cameraEventRequest
		if err := decodeEventBody(r, &req); err != nil {
			if isMaxBytesError(err) {
				WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
				return
			}
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}

		ev := session.Event{
			LotID:         req.LotID,
			CameraID:      req.CameraID,
			CameraEventID: req.CameraEventID,
			Direction:     session.Direction(req.Direction),
			PlateRaw:      req.PlateRaw,
			CapturedAt:    req.CapturedAt,
		}

		res, err := events.HandleEvent(r.Context(), ev)
		if err != nil {
			writeEventError(w, r, logger, err)
			return
		}

		status := http.StatusCreated
		if res.Replayed {
			status = http.StatusOK
		}
		WriteJSON(w, status, cameraEventResponse{
			EventID:            res.EventID,
			Outcome:            res.Outcome,
			SessionID:          optionalID(res.SessionID),
			ReviewID:           optionalID(res.ReviewID),
			AmountKurus:        res.AmountKurus,
			Replayed:           res.Replayed,
			ClosedByOrphanExit: res.ClosedByOrphanExit,
		})
	}
}

func decodeEventBody(r *http.Request, req *cameraEventRequest) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(req); err != nil {
		return err
	}

	var fazladan any
	if err := dec.Decode(&fazladan); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("govdeden sonra fazladan veri")
		}
		return err
	}
	return nil
}

func isMaxBytesError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func optionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

func writeEventError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, session.ErrInvalidEvent):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_event", err.Error())
	case errors.Is(err, sessionsvc.ErrLotNotFound):
		WriteError(w, http.StatusNotFound, "lot_not_found", "lot not found")
	case errors.Is(err, sessionsvc.ErrEventConflict):
		WriteError(w, http.StatusConflict, "event_conflict", "event conflict")
	default:
		logger.Error("kamera olayi islenemedi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
