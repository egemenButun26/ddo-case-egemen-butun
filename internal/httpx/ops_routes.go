package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"

	"parkakil/internal/auth"
	"parkakil/internal/domain/occupancy"
	"parkakil/internal/domain/plate"
	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/store"
)

const maxOpsBodyBytes = 16 << 10

var istanbulLocation = mustLocation("Europe/Istanbul")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func mountOpsRoutes(r chi.Router, deps Deps) {
	r.With(requireAuth(deps)).Get("/lots", opsLotsHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleAttendant, auth.RoleManager, auth.RoleAdmin)).
		Get("/occupancy", opsOccupancyHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAttendant, auth.RoleManager, auth.RoleAdmin)).
		Get("/events/recent", opsRecentEventsHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleAttendant, auth.RoleManager, auth.RoleAdmin)).
		Get("/reviews", opsListReviewsHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAttendant, auth.RoleAdmin)).
		Post("/reviews/{id}/resolve", opsResolveReviewHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleAttendant, auth.RoleManager, auth.RoleAdmin)).
		Get("/blacklist", opsListBlacklistHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleManager, auth.RoleAdmin)).
		Post("/blacklist", opsCreateBlacklistHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleManager, auth.RoleAdmin)).
		Delete("/blacklist/{id}", opsDeleteBlacklistHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleManager, auth.RoleAdmin)).
		Post("/reservations", opsCreateReservationHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleManager, auth.RoleAdmin)).
		Delete("/reservations/{id}", opsDeleteReservationHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/reports/daily", opsDailyReportHandler(deps))

	r.With(requireAuth(deps), requireRole(auth.RoleAdmin)).
		Post("/admin/occupancy/{lot_id}/recount", opsRecountHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAdmin, auth.RoleManager)).
		Get("/admin/counter-drifts", opsCounterDriftsHandler(deps))
}

type opsLotView struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

func opsLotsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lots, err := store.ListLots(r.Context(), deps.DB)
		if err != nil {
			opsInternalError(w, r, deps, "otopark listesi okunamadi", err)
			return
		}
		out := make([]opsLotView, 0, len(lots))
		for _, lot := range lots {
			out = append(out, opsLotView{ID: lot.ID, Code: lot.Code, Name: lot.Name, Capacity: lot.Capacity})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type opsOccupancyView struct {
	LotID              int64  `json:"lot_id"`
	Code               string `json:"code"`
	Capacity           int    `json:"capacity"`
	Occupied           int    `json:"occupied"`
	ActiveReservations int    `json:"active_reservations"`
	Available          int    `json:"available"`
}

func opsOccupancyHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, ok := opsOccupancyService(w, deps)
		if !ok {
			return
		}
		lots, err := store.ListLots(r.Context(), deps.DB)
		if err != nil {
			opsInternalError(w, r, deps, "otopark listesi okunamadi", err)
			return
		}
		out := make([]opsOccupancyView, 0, len(lots))
		for _, lot := range lots {
			snap, err := svc.Occupancy(r.Context(), lot.ID)
			if err != nil {
				opsInternalError(w, r, deps, "doluluk okunamadi", err)
				return
			}
			out = append(out, opsOccupancyView{
				LotID:              lot.ID,
				Code:               lot.Code,
				Capacity:           snap.Capacity,
				Occupied:           snap.Occupied,
				ActiveReservations: snap.ActiveReservations,
				Available:          snap.Available,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type opsReservationView struct {
	ID         int64            `json:"id"`
	LotID      int64            `json:"lot_id"`
	Plate      string           `json:"plate"`
	Status     occupancy.Status `json:"status"`
	ReservedAt time.Time        `json:"reserved_at"`
	ExpiresAt  time.Time        `json:"expires_at"`
	Replayed   bool             `json:"replayed"`
}

func viewReservation(r occupancy.Reservation) opsReservationView {
	return opsReservationView{
		ID:         r.ID,
		LotID:      r.LotID,
		Plate:      r.Plate,
		Status:     r.Status,
		ReservedAt: r.ReservedAt.UTC(),
		ExpiresAt:  r.ExpiresAt.UTC(),
	}
}

type opsReservationRequest struct {
	LotID int64  `json:"lot_id"`
	Plate string `json:"plate"`
}

func opsCreateReservationHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, ok := opsOccupancyService(w, deps)
		if !ok {
			return
		}
		anahtar := strings.TrimSpace(r.Header.Get(idempotencyHeader))
		if len(anahtar) > 255 {
			WriteError(w, http.StatusBadRequest, "invalid_idempotency_key", "invalid idempotency key")
			return
		}

		var req opsReservationRequest
		if !decodeOpsBody(w, r, &req) {
			return
		}
		if req.LotID <= 0 {
			WriteError(w, http.StatusBadRequest, "invalid_lot", "invalid lot_id")
			return
		}
		p := plate.Normalize(req.Plate)
		if p == "" {
			WriteError(w, http.StatusBadRequest, "invalid_plate", "invalid plate")
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if user.Role == auth.RoleDriver {
			if user.Plate == nil || plate.Normalize(*user.Plate) != p {
				WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
		}
		opts := occupancysvc.ReserveOptions{IdempotencyKey: anahtar}
		if anahtar != "" {
			opts.RequestHash = canonicalHash(req)
		}
		reservation, replayed, err := svc.ReserveWithOptions(r.Context(), req.LotID, p, time.Now(), opts)
		if err != nil {
			switch {
			case errors.Is(err, occupancysvc.ErrInvalidPlate):
				WriteError(w, http.StatusBadRequest, "invalid_plate", "invalid plate")
			case errors.Is(err, occupancysvc.ErrNoCapacity):
				WriteError(w, http.StatusConflict, "no_capacity", "no capacity")
			case errors.Is(err, occupancysvc.ErrReservationExists):
				WriteError(w, http.StatusConflict, "reservation_exists", "reservation already exists")
			case errors.Is(err, occupancysvc.ErrOpenSessionExists):
				WriteError(w, http.StatusConflict, "open_session_exists", "open session exists")
			case errors.Is(err, store.ErrIdempotencyConflict):
				WriteError(w, http.StatusConflict, "idempotency_conflict", "idempotency key reused with a different request")
			case errors.Is(err, store.ErrLotNotFound):
				WriteError(w, http.StatusNotFound, "lot_not_found", "lot not found")
			default:
				opsInternalError(w, r, deps, "rezervasyon olusturulamadi", err)
			}
			return
		}
		view := viewReservation(reservation)
		view.Replayed = replayed
		durum := http.StatusCreated
		if replayed {
			durum = http.StatusOK
		}
		WriteJSON(w, durum, view)
	}
}

func opsDeleteReservationHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, ok := opsOccupancyService(w, deps)
		if !ok {
			return
		}
		id, ok := parsePathID(w, r, "id")
		if !ok {
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		reservation, err := store.GetReservation(r.Context(), deps.DB, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			opsInternalError(w, r, deps, "rezervasyon okunamadi", err)
			return
		}
		if user.Role == auth.RoleDriver &&
			(user.Plate == nil || plate.Normalize(*user.Plate) != reservation.Plate) {
			WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
			return
		}
		if err := svc.Cancel(r.Context(), id, time.Now()); err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				WriteError(w, http.StatusNotFound, "not_found", "not found")
			case errors.Is(err, store.ErrReservationNotActive):
				WriteError(w, http.StatusConflict, "reservation_not_active", "reservation not active")
			default:
				opsInternalError(w, r, deps, "rezervasyon iptal edilemedi", err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type opsRecountResponse struct {
	Stored     int  `json:"stored"`
	Recomputed int  `json:"recomputed"`
	Drifted    bool `json:"drifted"`
	Corrected  bool `json:"corrected"`
}

func opsRecountHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, ok := opsOccupancyService(w, deps)
		if !ok {
			return
		}
		lotID, ok := parsePathID(w, r, "lot_id")
		if !ok {
			return
		}
		d, err := svc.DetectAndFixDrift(r.Context(), lotID, time.Now())
		if err != nil {
			if errors.Is(err, store.ErrLotNotFound) {
				WriteError(w, http.StatusNotFound, "lot_not_found", "lot not found")
				return
			}
			opsInternalError(w, r, deps, "sayac yeniden hesaplanamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, opsRecountResponse{
			Stored:     d.Stored,
			Recomputed: d.Recomputed,
			Drifted:    d.Drifted,
			Corrected:  d.Drifted,
		})
	}
}

type opsCounterDriftView struct {
	ID         int64     `json:"id"`
	LotID      int64     `json:"lot_id"`
	Stored     int       `json:"stored"`
	Recomputed int       `json:"recomputed"`
	DetectedAt time.Time `json:"detected_at"`
	Corrected  bool      `json:"corrected"`
}

type opsCounterDriftsResponse struct {
	Items       []opsCounterDriftView `json:"items"`
	NextAfterID *int64                `json:"next_after_id"`
}

func opsCounterDriftsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		afterID, err := parseAfterID(r.URL.Query().Get("after_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid after_id")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid limit")
			return
		}
		drifts, err := store.ListCounterDrifts(r.Context(), deps.DB, afterID, limit+1)
		if err != nil {
			opsInternalError(w, r, deps, "kayma kayitlari okunamadi", err)
			return
		}
		var next *int64
		if len(drifts) > limit {
			nextID := drifts[limit-1].ID
			next = &nextID
			drifts = drifts[:limit]
		}
		items := make([]opsCounterDriftView, 0, len(drifts))
		for _, d := range drifts {
			items = append(items, opsCounterDriftView{
				ID:         d.ID,
				LotID:      d.LotID,
				Stored:     d.Stored,
				Recomputed: d.Recomputed,
				DetectedAt: d.DetectedAt,
				Corrected:  d.Corrected,
			})
		}
		WriteJSON(w, http.StatusOK, opsCounterDriftsResponse{Items: items, NextAfterID: next})
	}
}

type opsBlacklistView struct {
	ID        int64      `json:"id"`
	Plate     string     `json:"plate"`
	Reason    string     `json:"reason"`
	Active    bool       `json:"active"`
	CreatedBy *int64     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	RemovedAt *time.Time `json:"removed_at"`
}

type opsBlacklistResponse struct {
	Items       []opsBlacklistView `json:"items"`
	NextAfterID *int64             `json:"next_after_id"`
}

func viewBlacklist(e store.BlacklistEntry) opsBlacklistView {
	return opsBlacklistView{
		ID:        e.ID,
		Plate:     e.Plate,
		Reason:    e.Reason,
		Active:    e.Active,
		CreatedBy: e.CreatedBy,
		CreatedAt: e.CreatedAt,
		RemovedAt: e.RemovedAt,
	}
}

func opsListBlacklistHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		afterID, err := parseAfterID(r.URL.Query().Get("after_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid after_id")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid limit")
			return
		}
		entries, err := store.ListBlacklist(r.Context(), deps.DB, afterID, limit+1)
		if err != nil {
			opsInternalError(w, r, deps, "kara liste okunamadi", err)
			return
		}
		var next *int64
		if len(entries) > limit {
			nextID := entries[limit-1].ID
			next = &nextID
			entries = entries[:limit]
		}
		items := make([]opsBlacklistView, 0, len(entries))
		for _, e := range entries {
			items = append(items, viewBlacklist(e))
		}
		WriteJSON(w, http.StatusOK, opsBlacklistResponse{Items: items, NextAfterID: next})
	}
}

type opsBlacklistRequest struct {
	Plate  string `json:"plate"`
	Reason string `json:"reason"`
}

func opsCreateBlacklistHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req opsBlacklistRequest
		if !decodeOpsBody(w, r, &req) {
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		entry, err := store.InsertBlacklist(r.Context(), deps.DB, req.Plate, req.Reason, &user.ID)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrInvalidBlacklist):
				WriteError(w, http.StatusBadRequest, "invalid_blacklist", "plate and reason required")
			case errors.Is(err, store.ErrBlacklistExists):
				WriteError(w, http.StatusConflict, "blacklist_exists", "plate already blacklisted")
			default:
				opsInternalError(w, r, deps, "kara liste kaydi yazilamadi", err)
			}
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "blacklist.create",
			Entity:      "blacklist",
			EntityID:    strconv.FormatInt(entry.ID, 10),
			NewValue:    map[string]any{"plate": entry.Plate, "reason": entry.Reason},
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			opsInternalError(w, r, deps, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusCreated, viewBlacklist(entry))
	}
}

func opsDeleteBlacklistHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r, "id")
		if !ok {
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if err := store.DeactivateBlacklist(r.Context(), deps.DB, id, time.Now()); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			opsInternalError(w, r, deps, "kara liste kaydi kaldirilamadi", err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "blacklist.remove",
			Entity:      "blacklist",
			EntityID:    strconv.FormatInt(id, 10),
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			opsInternalError(w, r, deps, "denetim kaydi yazilamadi", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type opsDailyReportResponse struct {
	Day              string `json:"day"`
	LotID            int64  `json:"lot_id"`
	Entries          int    `json:"entries"`
	Exits            int    `json:"exits"`
	ClosedSessions   int    `json:"closed_sessions"`
	TotalAmountKurus int64  `json:"total_amount_kurus"`
	CollectedKurus   int64  `json:"collected_kurus"`
	OpenDebtKurus    int64  `json:"open_debt_kurus"`
	OpenReviews      int    `json:"open_reviews"`
}

func opsDailyReportHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day := r.URL.Query().Get("day")
		start, err := time.ParseInLocation("2006-01-02", day, istanbulLocation)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_day", "invalid day")
			return
		}
		lotID, err := parseOptionalLotID(r.URL.Query().Get("lot_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_query", "invalid lot_id")
			return
		}
		report, err := store.GetDailyReport(r.Context(), deps.DB, lotID, start, start.AddDate(0, 0, 1))
		if err != nil {
			opsInternalError(w, r, deps, "gunluk rapor hesaplanamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, opsDailyReportResponse{
			Day:              start.Format("2006-01-02"),
			LotID:            lotID,
			Entries:          report.Entries,
			Exits:            report.Exits,
			ClosedSessions:   report.ClosedSessions,
			TotalAmountKurus: report.TotalAmountKurus,
			CollectedKurus:   report.CollectedKurus,
			OpenDebtKurus:    report.OpenDebtKurus,
			OpenReviews:      report.OpenReviews,
		})
	}
}

type opsRecentEventView struct {
	ID         int64     `json:"id"`
	LotID      int64     `json:"lot_id"`
	Direction  string    `json:"direction"`
	PlateRaw   string    `json:"plate_raw"`
	PlateNorm  string    `json:"plate_norm"`
	CapturedAt time.Time `json:"captured_at"`
	Outcome    string    `json:"outcome"`
	SessionID  *int64    `json:"session_id"`
}

type opsRecentEventsResponse struct {
	Items        []opsRecentEventView `json:"items"`
	NextBeforeID *int64               `json:"next_before_id"`
}

func opsRecentEventsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lotID, err := parseOptionalLotID(r.URL.Query().Get("lot_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_query", "invalid lot_id")
			return
		}
		beforeID, err := parseAfterID(r.URL.Query().Get("before_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid before_id")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid limit")
			return
		}
		events, err := store.ListRecentEvents(r.Context(), deps.DB, lotID, beforeID, limit+1)
		if err != nil {
			opsInternalError(w, r, deps, "son olaylar okunamadi", err)
			return
		}
		var next *int64
		if len(events) > limit {
			nextID := events[limit-1].ID
			next = &nextID
			events = events[:limit]
		}
		items := make([]opsRecentEventView, 0, len(events))
		for _, ev := range events {
			items = append(items, opsRecentEventView{
				ID:         ev.ID,
				LotID:      ev.LotID,
				Direction:  string(ev.Direction),
				PlateRaw:   ev.PlateRaw,
				PlateNorm:  ev.PlateNorm,
				CapturedAt: ev.CapturedAt,
				Outcome:    string(ev.Outcome),
				SessionID:  optionalID(ev.SessionID),
			})
		}
		WriteJSON(w, http.StatusOK, opsRecentEventsResponse{Items: items, NextBeforeID: next})
	}
}

type opsReviewView struct {
	ID                 int64      `json:"id"`
	EventID            int64      `json:"event_id"`
	LotID              int64      `json:"lot_id"`
	Reason             string     `json:"reason"`
	ReadPlate          string     `json:"read_plate"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	ResolvedAt         *time.Time `json:"resolved_at"`
	SuggestedSessionID *int64     `json:"suggested_session_id"`
	PlateRaw           string     `json:"plate_raw"`
	PlateNorm          string     `json:"plate_norm"`
	CapturedAt         time.Time  `json:"captured_at"`
}

type opsReviewsResponse struct {
	Items       []opsReviewView `json:"items"`
	NextAfterID *int64          `json:"next_after_id"`
}

func opsListReviewsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "open"
		}
		if status != "open" && status != "resolved" {
			WriteError(w, http.StatusBadRequest, "invalid_status", "invalid status")
			return
		}
		afterID, err := parseAfterID(r.URL.Query().Get("after_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid after_id")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid limit")
			return
		}
		reviews, err := store.ListReviews(r.Context(), deps.DB, status, afterID, limit+1)
		if err != nil {
			opsInternalError(w, r, deps, "inceleme kuyrugu okunamadi", err)
			return
		}
		var next *int64
		if len(reviews) > limit {
			nextID := reviews[limit-1].ID
			next = &nextID
			reviews = reviews[:limit]
		}
		items := make([]opsReviewView, 0, len(reviews))
		for _, rv := range reviews {
			items = append(items, opsReviewView{
				ID:                 rv.ID,
				EventID:            rv.EventID,
				LotID:              rv.LotID,
				Reason:             rv.Reason,
				ReadPlate:          rv.ReadPlate,
				Status:             rv.Status,
				CreatedAt:          rv.CreatedAt,
				ResolvedAt:         rv.ResolvedAt,
				SuggestedSessionID: rv.SuggestedSessionID,
				PlateRaw:           rv.PlateRaw,
				PlateNorm:          rv.PlateNorm,
				CapturedAt:         rv.CapturedAt,
			})
		}
		WriteJSON(w, http.StatusOK, opsReviewsResponse{Items: items, NextAfterID: next})
	}
}

type opsResolveReviewRequest struct {
	Action    string `json:"action"`
	SessionID int64  `json:"session_id"`
}

type opsResolveReviewResponse struct {
	ID         int64     `json:"id"`
	Status     string    `json:"status"`
	ResolvedAt time.Time `json:"resolved_at"`
}

func opsResolveReviewHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r, "id")
		if !ok {
			return
		}
		var req opsResolveReviewRequest
		if !decodeOpsBody(w, r, &req) {
			return
		}
		if req.Action != "dismiss" && req.Action != "link_session" {
			WriteError(w, http.StatusBadRequest, "invalid_action", "invalid action")
			return
		}
		if req.Action == "link_session" && req.SessionID <= 0 {
			WriteError(w, http.StatusBadRequest, "invalid_action", "session_id required")
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		review, err := store.GetReview(r.Context(), deps.DB, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			opsInternalError(w, r, deps, "inceleme okunamadi", err)
			return
		}
		if review.Status != "open" {
			WriteError(w, http.StatusConflict, "review_not_open", "review already resolved")
			return
		}
		if req.Action == "link_session" {
			sess, err := store.GetSession(r.Context(), deps.DB, req.SessionID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					WriteError(w, http.StatusNotFound, "session_not_found", "session not found")
					return
				}
				opsInternalError(w, r, deps, "oturum okunamadi", err)
				return
			}
			if sess.LotID != review.LotID {
				WriteError(w, http.StatusBadRequest, "session_lot_mismatch", "session belongs to another lot")
				return
			}
		}
		now := time.Now()
		if err := store.ResolveReview(r.Context(), deps.DB, id, now); err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				WriteError(w, http.StatusNotFound, "not_found", "not found")
			case errors.Is(err, store.ErrReviewNotOpen):
				WriteError(w, http.StatusConflict, "review_not_open", "review already resolved")
			default:
				opsInternalError(w, r, deps, "inceleme cozulemedi", err)
			}
			return
		}
		if req.Action == "link_session" {
			if err := store.LinkReviewEvent(r.Context(), deps.DB, review.EventID, req.SessionID); err != nil {
				opsInternalError(w, r, deps, "olay oturuma baglanamadi", err)
				return
			}
		}
		newValue := map[string]any{"action": req.Action}
		if req.Action == "link_session" {
			newValue["session_id"] = req.SessionID
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "review.resolve",
			Entity:      "review",
			EntityID:    strconv.FormatInt(id, 10),
			NewValue:    newValue,
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			opsInternalError(w, r, deps, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, opsResolveReviewResponse{
			ID:         id,
			Status:     "resolved",
			ResolvedAt: now.UTC(),
		})
	}
}

func opsOccupancyService(w http.ResponseWriter, deps Deps) (*occupancysvc.Service, bool) {
	if deps.Occupancy == nil {
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return nil, false
	}
	return deps.Occupancy, true
}

func opsInternalError(w http.ResponseWriter, r *http.Request, deps Deps, message string, err error) {
	if deps.Logger != nil {
		deps.Logger.Error(message, "hata", err, "request_id", r.Header.Get("X-Request-ID"))
	}
	WriteError(w, http.StatusInternalServerError, "internal", "internal error")
}

func decodeOpsBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxOpsBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return false
	}
	return true
}

func parsePathID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func parseOptionalLotID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("gecersiz lot_id")
	}
	return id, nil
}
