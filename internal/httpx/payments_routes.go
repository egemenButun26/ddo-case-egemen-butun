package httpx

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"parkakil/internal/auth"
	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/paymentsvc"
	"parkakil/internal/store"
)

const (
	maxPaymentBodyBytes = 64 << 10
	idempotencyHeader   = "Idempotency-Key"
	providerSignature   = "X-Provider-Signature"
	providerCallTimeout = 5 * time.Second
)

type kioskQuoteResponse struct {
	SessionID       int64        `json:"session_id"`
	Plate           string       `json:"plate"`
	EnteredAt       time.Time    `json:"entered_at"`
	TotalKurus      tariff.Kurus `json:"total_kurus"`
	PaidKurus       tariff.Kurus `json:"paid_kurus"`
	DueKurus        tariff.Kurus `json:"due_kurus"`
	PermitExpiresAt *time.Time   `json:"permit_expires_at"`
}

type createPaymentRequest struct {
	SessionID   int64        `json:"session_id"`
	AmountKurus tariff.Kurus `json:"amount_kurus"`
}

type kioskPaymentResponse struct {
	PaymentID   int64        `json:"payment_id"`
	SessionID   int64        `json:"session_id"`
	ProviderRef string       `json:"provider_ref"`
	AmountKurus tariff.Kurus `json:"amount_kurus"`
	Status      string       `json:"status"`
	Replayed    bool         `json:"replayed"`
}

type providerChargeRequest struct {
	ProviderRef string `json:"provider_ref"`
	AmountKurus int64  `json:"amount_kurus"`
	CallbackURL string `json:"callback_url"`
}

type notificationRequest struct {
	ProviderRef string       `json:"provider_ref"`
	Outcome     string       `json:"outcome"`
	AmountKurus tariff.Kurus `json:"amount_kurus"`
	ProviderAt  time.Time    `json:"provider_at"`
}

type notificationResponse struct {
	PaymentID      int64  `json:"payment_id"`
	SessionID      int64  `json:"session_id"`
	Status         string `json:"status"`
	Duplicate      bool   `json:"duplicate"`
	LateApproval   bool   `json:"late_approval"`
	RefundRequired bool   `json:"refund_required"`
	RefundID       *int64 `json:"refund_id"`
}

type gateDecisionResponse struct {
	Action          payment.Action `json:"action"`
	Reason          string         `json:"reason"`
	DueKurus        tariff.Kurus   `json:"due_kurus"`
	PriorDebtKurus  tariff.Kurus   `json:"prior_debt_kurus"`
	PermitExpiresAt *time.Time     `json:"permit_expires_at"`
	SessionID       *int64         `json:"session_id"`
}

type paymentView struct {
	ID           int64        `json:"id"`
	SessionID    int64        `json:"session_id"`
	AmountKurus  tariff.Kurus `json:"amount_kurus"`
	Status       string       `json:"status"`
	ProviderRef  string       `json:"provider_ref"`
	RequestedAt  time.Time    `json:"requested_at"`
	PaidAt       *time.Time   `json:"paid_at"`
	ConfirmedAt  *time.Time   `json:"confirmed_at"`
	LateApproval bool         `json:"late_approval"`
}

type paymentsResponse struct {
	Items       []paymentView `json:"items"`
	NextAfterID *int64        `json:"next_after_id"`
}

type refundRequest struct {
	PaymentID   int64        `json:"payment_id"`
	AmountKurus tariff.Kurus `json:"amount_kurus"`
	Reason      string       `json:"reason"`
}

type refundView struct {
	ID          int64        `json:"id"`
	PaymentID   int64        `json:"payment_id"`
	AmountKurus tariff.Kurus `json:"amount_kurus"`
	Status      string       `json:"status"`
	Reason      string       `json:"reason"`
	RequestedAt time.Time    `json:"requested_at"`
	Replayed    bool         `json:"replayed"`
}

type debtView struct {
	ID        int64        `json:"id"`
	SessionID int64        `json:"session_id"`
	LotID     int64        `json:"lot_id"`
	Amount    tariff.Kurus `json:"amount_kurus"`
	Paid      tariff.Kurus `json:"paid_kurus"`
	Remaining tariff.Kurus `json:"remaining_kurus"`
	Status    string       `json:"status"`
}

type debtsResponse struct {
	Plate      string       `json:"plate"`
	Items      []debtView   `json:"items"`
	TotalKurus tariff.Kurus `json:"total_kurus"`
}

func mountPaymentRoutes(r chi.Router, deps Deps) {
	if deps.Payments == nil {
		return
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	r.With(requireCameraKey(deps.CameraKey, logger)).Get("/kiosk/quote", kioskQuoteHandler(deps, logger))
	r.With(requireCameraKey(deps.CameraKey, logger)).Post("/kiosk/payments", kioskPaymentHandler(deps, logger))
	r.Post("/provider/notifications", providerNotificationHandler(deps, logger))
	r.With(requireCameraKey(deps.CameraKey, logger)).Get("/gate/decision", gateDecisionHandler(deps, logger))

	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/payments", paymentsListHandler(deps, logger))
	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleAdmin)).
		Post("/refunds", refundCreateHandler(deps, logger))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleAttendant, auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/debts", debtsHandler(deps, logger))
}

func kioskQuoteHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lotID, ok := parsePositiveInt64(r.URL.Query().Get("lot_id"))
		if !ok {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid lot_id")
			return
		}
		hamPlaka := strings.TrimSpace(r.URL.Query().Get("plate"))
		if hamPlaka == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "plate required")
			return
		}

		sess, durum, kod := matchOpenSession(r, deps, lotID, hamPlaka)
		if durum != 0 {
			WriteError(w, durum, kod, kodMessage(kod))
			return
		}

		simdi := time.Now().UTC()
		quote, err := deps.Payments.Quote(r.Context(), sess.ID, simdi)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrSessionNotOpen) {
				WriteError(w, http.StatusNotFound, "session_not_found", "session not found")
				return
			}
			writePaymentError(w, r, logger, err)
			return
		}

		var izin *time.Time
		if !quote.PermitExpiresAt.IsZero() {
			t := quote.PermitExpiresAt
			izin = &t
		}
		WriteJSON(w, http.StatusOK, kioskQuoteResponse{
			SessionID:       sess.ID,
			Plate:           sess.Plate,
			EnteredAt:       sess.EnteredAt,
			TotalKurus:      quote.Total,
			PaidKurus:       quote.Paid,
			DueKurus:        quote.Due,
			PermitExpiresAt: izin,
		})
	}
}

// matchOpenSession acik oturumlarda plakayi eslestirir. Hata durumunda HTTP
// durum kodu ve hata kodunu doner; durum 0 ise eslesme basarilidir.
func matchOpenSession(r *http.Request, deps Deps, lotID int64, hamPlaka string) (store.StoredSession, int, string) {
	acik, err := store.ListOpenSessions(r.Context(), deps.DB, lotID)
	if err != nil {
		return store.StoredSession{}, http.StatusInternalServerError, "internal"
	}
	adaylar := make([]plate.Candidate, 0, len(acik))
	for _, o := range acik {
		adaylar = append(adaylar, plate.Candidate{ID: strconv.FormatInt(o.ID, 10), Plate: o.Plate})
	}
	m, err := plate.Match(hamPlaka, adaylar, plate.DefaultThresholds)
	if err != nil {
		return store.StoredSession{}, http.StatusInternalServerError, "internal"
	}
	switch m.Decision {
	case plate.DecisionExact, plate.DecisionAuto:
	case plate.DecisionReview:
		if strings.Contains(m.Reason, "ambiguous") {
			return store.StoredSession{}, http.StatusConflict, "ambiguous_plate"
		}
		return store.StoredSession{}, http.StatusNotFound, "session_not_found"
	default:
		return store.StoredSession{}, http.StatusNotFound, "session_not_found"
	}
	if m.Best == nil {
		return store.StoredSession{}, http.StatusNotFound, "session_not_found"
	}
	id, err := strconv.ParseInt(m.Best.ID, 10, 64)
	if err != nil {
		return store.StoredSession{}, http.StatusInternalServerError, "internal"
	}
	sess, err := store.GetSession(r.Context(), deps.DB, id)
	if err != nil {
		return store.StoredSession{}, http.StatusNotFound, "session_not_found"
	}
	if sess.Status != "open" {
		return store.StoredSession{}, http.StatusNotFound, "session_not_found"
	}
	return sess, 0, ""
}

func kodMessage(kod string) string {
	switch kod {
	case "ambiguous_plate":
		return "ambiguous plate"
	case "session_not_found":
		return "session not found"
	default:
		return "internal error"
	}
}

func kioskPaymentHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !jsonContentType(r) {
			WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
			return
		}
		anahtar := r.Header.Get(idempotencyHeader)
		if anahtar == "" || len(anahtar) > 255 {
			WriteError(w, http.StatusBadRequest, "idempotency_key_required", "idempotency key required")
			return
		}

		var istek createPaymentRequest
		if !decodePaymentBody(w, r, &istek) {
			return
		}
		if istek.SessionID <= 0 || istek.AmountKurus <= 0 {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid payment request")
			return
		}

		hash := canonicalHash(istek)
		ref, err := newProviderRef()
		if err != nil {
			logger.Error("saglayici referansi uretilemedi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}

		simdi := time.Now().UTC()
		res, err := deps.Payments.CreatePayment(r.Context(), paymentsvc.CreateReq{
			SessionID:      istek.SessionID,
			Amount:         istek.AmountKurus,
			IdempotencyKey: anahtar,
			RequestHash:    hash,
			ProviderRef:    ref,
			Now:            simdi,
		})
		if err != nil {
			writePaymentError(w, r, logger, err)
			return
		}

		if !res.Replayed {
			appendPaymentAudit(deps, logger, r, store.AuditEntry{
				ActorKind: store.ActorCamera,
				Action:    "payment.created",
				Entity:    "payment",
				EntityID:  strconv.FormatInt(res.PaymentID, 10),
				NewValue: map[string]any{
					"session_id":   res.SessionID,
					"amount_kurus": res.Payment.Amount,
					"provider_ref": res.Payment.ProviderRef,
					"status":       string(res.Payment.Status),
				},
			})
			notifyProvider(deps, logger, r, res.Payment.ProviderRef, res.Payment.Amount)
		}

		durum := http.StatusCreated
		if res.Replayed {
			durum = http.StatusOK
		}
		WriteJSON(w, durum, kioskPaymentResponse{
			PaymentID:   res.PaymentID,
			SessionID:   res.SessionID,
			ProviderRef: res.Payment.ProviderRef,
			AmountKurus: res.Payment.Amount,
			Status:      string(res.Payment.Status),
			Replayed:    res.Replayed,
		})
	}
}

func providerNotificationHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPaymentBodyBytes))
		if err != nil {
			if isMaxBytesError(err) {
				WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
				return
			}
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		if !verifyProviderSignature(deps.WebhookSecret, r.Header.Get(providerSignature), body) {
			WriteError(w, http.StatusUnauthorized, "invalid_signature", "invalid signature")
			return
		}

		var istek notificationRequest
		if err := json.Unmarshal(body, &istek); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}

		simdi := time.Now().UTC()
		res, err := deps.Payments.ApplyNotification(r.Context(), payment.Notification{
			ProviderRef: istek.ProviderRef,
			Outcome:     payment.Status(istek.Outcome),
			Amount:      istek.AmountKurus,
			ProviderAt:  istek.ProviderAt,
			ReceivedAt:  simdi,
		})
		if err != nil {
			writePaymentError(w, r, logger, err)
			return
		}

		appendPaymentAudit(deps, logger, r, store.AuditEntry{
			ActorKind: store.ActorProvider,
			Action:    "payment.notified",
			Entity:    "payment",
			EntityID:  strconv.FormatInt(res.PaymentID, 10),
			NewValue: map[string]any{
				"provider_ref":  res.Payment.ProviderRef,
				"status":        string(res.Payment.Status),
				"duplicate":     res.Transition.Duplicate,
				"late_approval": res.Transition.LateApproval,
				"session_id":    res.SessionID,
			},
		})

		var iadeID *int64
		if res.RefundRequired {
			iade, err := deps.Payments.CreateRefund(r.Context(), paymentsvc.RefundReq{
				PaymentID:      res.PaymentID,
				Amount:         res.Overpaid,
				Reason:         "overpaid",
				IdempotencyKey: "auto-refund-" + res.Payment.ProviderRef,
				RequestHash: canonicalHash(refundRequest{
					PaymentID:   res.PaymentID,
					AmountKurus: res.Overpaid,
					Reason:      "overpaid",
				}),
				Now: simdi,
			})
			if err != nil {
				logger.Error("otomatik iade olusturulamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
				WriteError(w, http.StatusInternalServerError, "internal", "internal error")
				return
			}
			if !iade.Replayed {
				appendPaymentAudit(deps, logger, r, store.AuditEntry{
					ActorKind: store.ActorProvider,
					Action:    "refund.created",
					Entity:    "refund",
					EntityID:  strconv.FormatInt(iade.Refund.ID, 10),
					NewValue: map[string]any{
						"payment_id":   iade.Refund.PaymentID,
						"amount_kurus": iade.Refund.Amount,
						"reason":       iade.Refund.Reason,
						"automatic":    true,
					},
				})
			}
			id := iade.Refund.ID
			iadeID = &id
		}

		WriteJSON(w, http.StatusOK, notificationResponse{
			PaymentID:      res.PaymentID,
			SessionID:      res.SessionID,
			Status:         string(res.Payment.Status),
			Duplicate:      res.Transition.Duplicate,
			LateApproval:   res.Transition.LateApproval,
			RefundRequired: res.RefundRequired,
			RefundID:       iadeID,
		})
	}
}

func gateDecisionHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lotID, ok := parsePositiveInt64(r.URL.Query().Get("lot_id"))
		if !ok {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid lot_id")
			return
		}
		hamPlaka := strings.TrimSpace(r.URL.Query().Get("plate"))
		if hamPlaka == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "plate required")
			return
		}

		karar, sessionID, err := deps.Payments.DecideGate(r.Context(), lotID, hamPlaka, time.Now().UTC())
		if err != nil {
			writePaymentError(w, r, logger, err)
			return
		}
		var izin *time.Time
		if !karar.PermitExpiresAt.IsZero() {
			t := karar.PermitExpiresAt
			izin = &t
		}
		var oturum *int64
		if sessionID > 0 {
			id := sessionID
			oturum = &id
		}
		WriteJSON(w, http.StatusOK, gateDecisionResponse{
			Action:          karar.Action,
			Reason:          karar.Reason,
			DueKurus:        karar.Due,
			PriorDebtKurus:  karar.PriorDebt,
			PermitExpiresAt: izin,
			SessionID:       oturum,
		})
	}
}

func paymentsListHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
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
		items, err := store.ListPayments(r.Context(), deps.DB, afterID, limit+1)
		if err != nil {
			logger.Error("odeme listesi okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		var next *int64
		if len(items) > limit {
			nextID := items[limit-1].ID
			next = &nextID
			items = items[:limit]
		}
		out := make([]paymentView, 0, len(items))
		for _, it := range items {
			out = append(out, viewPayment(it))
		}
		WriteJSON(w, http.StatusOK, paymentsResponse{Items: out, NextAfterID: next})
	}
}

func refundCreateHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !jsonContentType(r) {
			WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
			return
		}
		anahtar := r.Header.Get(idempotencyHeader)
		if anahtar == "" || len(anahtar) > 255 {
			WriteError(w, http.StatusBadRequest, "idempotency_key_required", "idempotency key required")
			return
		}
		var istek refundRequest
		if !decodePaymentBody(w, r, &istek) {
			return
		}
		if istek.PaymentID <= 0 || istek.AmountKurus <= 0 || istek.Reason != "manual" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid refund request")
			return
		}

		res, err := deps.Payments.CreateRefund(r.Context(), paymentsvc.RefundReq{
			PaymentID:      istek.PaymentID,
			Amount:         istek.AmountKurus,
			Reason:         istek.Reason,
			IdempotencyKey: anahtar,
			RequestHash:    canonicalHash(istek),
			Now:            time.Now().UTC(),
		})
		if err != nil {
			writePaymentError(w, r, logger, err)
			return
		}
		if !res.Replayed {
			user, _ := UserFromContext(r.Context())
			var actor *int64
			if user.ID > 0 {
				id := user.ID
				actor = &id
			}
			appendPaymentAudit(deps, logger, r, store.AuditEntry{
				ActorUserID: actor,
				ActorKind:   store.ActorUser,
				Action:      "refund.created",
				Entity:      "refund",
				EntityID:    strconv.FormatInt(res.Refund.ID, 10),
				NewValue: map[string]any{
					"payment_id":   res.Refund.PaymentID,
					"amount_kurus": res.Refund.Amount,
					"reason":       res.Refund.Reason,
				},
			})
		}

		durum := http.StatusCreated
		if res.Replayed {
			durum = http.StatusOK
		}
		WriteJSON(w, durum, refundView{
			ID:          res.Refund.ID,
			PaymentID:   res.Refund.PaymentID,
			AmountKurus: res.Refund.Amount,
			Status:      res.Refund.Status,
			Reason:      res.Refund.Reason,
			RequestedAt: res.Refund.RequestedAt,
			Replayed:    res.Replayed,
		})
	}
}

func debtsHandler(deps Deps, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		hamPlaka := strings.TrimSpace(r.URL.Query().Get("plate"))
		if user.Role == auth.RoleDriver {
			if user.Plate == nil || strings.TrimSpace(*user.Plate) == "" {
				WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			if hamPlaka != "" && plate.Normalize(hamPlaka) != plate.Normalize(*user.Plate) {
				WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			hamPlaka = *user.Plate
		}
		if hamPlaka == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "plate required")
			return
		}

		borclar, err := store.ListOpenDebtsByPlate(r.Context(), deps.DB, hamPlaka)
		if err != nil {
			logger.Error("borc listesi okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		out := make([]debtView, 0, len(borclar))
		var toplam tariff.Kurus
		for _, b := range borclar {
			out = append(out, debtView{
				ID:        b.ID,
				SessionID: b.SessionID,
				LotID:     b.LotID,
				Amount:    b.Amount,
				Paid:      b.Paid,
				Remaining: b.Remaining,
				Status:    b.Status,
			})
			toplam += b.Remaining
		}
		WriteJSON(w, http.StatusOK, debtsResponse{
			Plate:      plate.Normalize(hamPlaka),
			Items:      out,
			TotalKurus: toplam,
		})
	}
}

func notifyProvider(deps Deps, logger *slog.Logger, r *http.Request, ref string, amount tariff.Kurus) {
	if deps.ProviderURL == "" {
		return
	}
	callback := strings.TrimRight(deps.PublicAPIURL, "/") + "/provider/notifications"
	body, err := json.Marshal(providerChargeRequest{
		ProviderRef: ref,
		AmountKurus: int64(amount),
		CallbackURL: callback,
	})
	if err != nil {
		logger.Error("saglayici istegi hazirlanamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		return
	}
	u, err := url.JoinPath(deps.ProviderURL, "charges")
	if err != nil {
		logger.Error("saglayici adresi gecersiz", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		return
	}
	client := &http.Client{Timeout: providerCallTimeout}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		logger.Error("saglayici istegi olusturulamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("saglayiciya ulasilamadi; odeme beklemede", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("saglayici istegi reddetti; odeme beklemede",
			"durum", resp.StatusCode, "request_id", r.Header.Get("X-Request-ID"))
	}
}

func verifyProviderSignature(secret, header string, body []byte) bool {
	if secret == "" || header == "" {
		return false
	}
	parcalar := strings.SplitN(header, "=", 2)
	if len(parcalar) != 2 || parcalar[0] != "sha256" {
		return false
	}
	gelen, err := hex.DecodeString(parcalar[1])
	if err != nil || len(gelen) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	beklenen := mac.Sum(nil)
	return subtle.ConstantTimeCompare(beklenen, gelen) == 1
}

func signProviderBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func canonicalHash(v any) [32]byte {
	b, _ := json.Marshal(v)
	return sha256.Sum256(b)
}

func newProviderRef() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "prv-" + hex.EncodeToString(buf), nil
}

func jsonContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func decodePaymentBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxPaymentBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if isMaxBytesError(err) {
			WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			return false
		}
		WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return false
	}
	var fazladan any
	if err := dec.Decode(&fazladan); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return false
	}
	return true
}

func parsePositiveInt64(raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func appendPaymentAudit(deps Deps, logger *slog.Logger, r *http.Request, entry store.AuditEntry) {
	entry.RequestID = r.Header.Get("X-Request-ID")
	if err := store.AppendAudit(r.Context(), deps.DB, entry); err != nil {
		logger.Error("denetim kaydi yazilamadi", "hata", err, "request_id", entry.RequestID)
	}
}

func viewPayment(sp store.StoredPayment) paymentView {
	out := paymentView{
		ID:           sp.ID,
		SessionID:    sp.SessionID,
		AmountKurus:  sp.Payment.Amount,
		Status:       string(sp.Payment.Status),
		ProviderRef:  sp.Payment.ProviderRef,
		RequestedAt:  sp.Payment.RequestedAt,
		LateApproval: sp.LateApproval,
	}
	if !sp.Payment.PaidAt.IsZero() {
		t := sp.Payment.PaidAt
		out.PaidAt = &t
	}
	if !sp.Payment.ConfirmedAt.IsZero() {
		t := sp.Payment.ConfirmedAt
		out.ConfirmedAt = &t
	}
	return out
}

func writePaymentError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrSessionNotOpen):
		WriteError(w, http.StatusNotFound, "session_not_found", "session not found")
	case errors.Is(err, store.ErrIdempotencyConflict):
		WriteError(w, http.StatusConflict, "idempotency_conflict", "idempotency conflict")
	case errors.Is(err, payment.ErrAmountMismatch):
		WriteError(w, http.StatusConflict, "amount_mismatch", "amount mismatch")
	case errors.Is(err, paymentsvc.ErrNothingDue):
		WriteError(w, http.StatusConflict, "nothing_due", "nothing due")
	case errors.Is(err, store.ErrPendingPaymentExists):
		WriteError(w, http.StatusConflict, "payment_pending", "payment pending")
	case errors.Is(err, paymentsvc.ErrRefundExceedsPayment):
		WriteError(w, http.StatusConflict, "refund_exceeds_payment", "refund exceeds payment")
	case errors.Is(err, paymentsvc.ErrRefundNotSucceeded):
		WriteError(w, http.StatusConflict, "refund_not_allowed", "refund not allowed")
	case errors.Is(err, payment.ErrNotificationConflict):
		WriteError(w, http.StatusConflict, "notification_conflict", "notification conflict")
	case errors.Is(err, store.ErrPaymentStateConflict):
		WriteError(w, http.StatusConflict, "notification_conflict", "notification conflict")
	case errors.Is(err, payment.ErrInvalidNotification),
		errors.Is(err, payment.ErrProviderRefMismatch),
		errors.Is(err, payment.ErrInvalidPayment),
		errors.Is(err, payment.ErrInvalidAmount):
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	default:
		logger.Error("odeme islemi basarisiz", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
