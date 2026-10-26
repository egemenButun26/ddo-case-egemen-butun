package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"parkakil/internal/auth"
	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/billingsvc"
	"parkakil/internal/store"
)

const maxBillingBodyBytes = 64 << 10

type planView struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	LotID   int64  `json:"lot_id"`
	Monthly int64  `json:"monthly_price_kurus"`
	Active  bool   `json:"active"`
}

type plansResponse struct {
	Items []planView `json:"items"`
}

type subscriptionView struct {
	ID                 int64      `json:"id"`
	PlanID             int64      `json:"plan_id"`
	PlanCode           string     `json:"plan_code"`
	MonthlyPrice       int64      `json:"monthly_price_kurus"`
	LotID              int64      `json:"lot_id"`
	Plate              string     `json:"plate"`
	UserID             *int64     `json:"user_id"`
	Status             string     `json:"status"`
	CurrentPeriodStart time.Time  `json:"current_period_start"`
	CurrentPeriodEnd   time.Time  `json:"current_period_end"`
	CancelledAt        *time.Time `json:"cancelled_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type subscriptionsResponse struct {
	Items       []subscriptionView `json:"items"`
	NextAfterID *int64             `json:"next_after_id"`
}

type cancelSubscriptionResponse struct {
	SubscriptionID int64  `json:"subscription_id"`
	Status         string `json:"status"`
	Credit         int64  `json:"credit_kurus"`
	InvoiceID      int64  `json:"invoice_id"`
}

type changePlanResponse struct {
	SubscriptionID  int64  `json:"subscription_id"`
	NewPlanCode     string `json:"new_plan_code"`
	Charge          int64  `json:"charge_kurus"`
	Credit          int64  `json:"credit_kurus"`
	Net             int64  `json:"net_kurus"`
	Direction       string `json:"direction"`
	ChargeInvoiceID int64  `json:"charge_invoice_id"`
	CreditInvoiceID int64  `json:"credit_invoice_id"`
}

type invoiceView struct {
	ID             int64      `json:"id"`
	SubscriptionID int64      `json:"subscription_id"`
	PeriodStart    time.Time  `json:"period_start"`
	PeriodEnd      time.Time  `json:"period_end"`
	Kind           string     `json:"kind"`
	Amount         int64      `json:"amount_kurus"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	NextRetryAt    *time.Time `json:"next_retry_at"`
	PaidAt         *time.Time `json:"paid_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type invoicesResponse struct {
	Items       []invoiceView `json:"items"`
	NextAfterID *int64        `json:"next_after_id"`
}

type periodEndRequest struct {
	PeriodStart string `json:"period_start"`
}

type periodEndResponse struct {
	InvoicesCreated int `json:"invoices_created"`
}

type paymentAttemptRequest struct {
	Success *bool `json:"success"`
}

type paymentAttemptResponse struct {
	InvoiceID          int64      `json:"invoice_id"`
	SubscriptionID     int64      `json:"subscription_id"`
	Status             string     `json:"status"`
	Attempts           int        `json:"attempts"`
	NextRetryAt        *time.Time `json:"next_retry_at"`
	SubscriptionStatus string     `json:"subscription_status"`
}

type providerSettlementRequest struct {
	ProviderRef string `json:"provider_ref"`
	Amount      int64  `json:"amount_kurus"`
}

type reconciliationImportResponse struct {
	Imported int            `json:"imported"`
	Report   billing.Report `json:"report"`
}

func mountBillingRoutes(r chi.Router, deps Deps) {
	r.With(requireAuth(deps)).Get("/plans", plansHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleManager, auth.RoleAdmin)).
		Post("/subscriptions", createSubscriptionHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/subscriptions", listSubscriptionsHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleManager, auth.RoleAdmin)).
		Post("/subscriptions/{id}/cancel", cancelSubscriptionHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleManager, auth.RoleAdmin)).
		Post("/subscriptions/{id}/change-plan", changePlanHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleDriver, auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/invoices", listInvoicesHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAdmin, auth.RoleAccountant)).
		Post("/billing/period-end", periodEndHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleAdmin)).
		Post("/invoices/{id}/payment-attempts", paymentAttemptHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleAdmin)).
		Post("/reconciliation/{day}/provider-report", reconciliationImportHandler(deps))
	r.With(requireAuth(deps), requireRole(auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin)).
		Get("/reconciliation/{day}", reconciliationReportHandler(deps))
}

func plansHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plans, err := store.ListPlans(r.Context(), deps.DB)
		if err != nil {
			billingInternalError(deps, w, r, "plan listesi okunamadi", err)
			return
		}
		items := make([]planView, 0, len(plans))
		for _, p := range plans {
			items = append(items, viewPlan(p))
		}
		WriteJSON(w, http.StatusOK, plansResponse{Items: items})
	}
}

func createSubscriptionHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		var req struct {
			Plate    string `json:"plate"`
			PlanCode string `json:"plan_code"`
		}
		if err := decodeBillingJSON(w, r, &req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		plate := strings.TrimSpace(req.Plate)
		planCode := strings.TrimSpace(req.PlanCode)
		if plate == "" || planCode == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "plate ve plan_code zorunlu")
			return
		}
		var ownerID *int64
		if user.Role == auth.RoleDriver {
			if user.Plate == nil || *user.Plate != plate {
				WriteError(w, http.StatusForbidden, "forbidden", "baska plaka icin abonelik acilamaz")
				return
			}
			id := user.ID
			ownerID = &id
		}

		sub, err := deps.Billing.SubscribeForUser(r.Context(), plate, planCode, ownerID, time.Now())
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.subscription_created",
			Entity:      "subscription",
			EntityID:    strconv.FormatInt(sub.ID, 10),
			NewValue:    map[string]any{"plate": sub.Plate, "plan_code": sub.PlanCode},
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusCreated, viewSubscription(sub))
	}
}

func listSubscriptionsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
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

		filter := store.SubscriptionFilter{AfterID: afterID, Limit: limit + 1}
		rawPlate := strings.TrimSpace(r.URL.Query().Get("plate"))
		if user.Role == auth.RoleDriver {
			if user.Plate == nil || *user.Plate == "" {
				WriteJSON(w, http.StatusOK, subscriptionsResponse{Items: []subscriptionView{}})
				return
			}
			if rawPlate != "" && rawPlate != *user.Plate {
				WriteError(w, http.StatusForbidden, "forbidden", "baska plaka listelenemez")
				return
			}
			plate := *user.Plate
			ownerID := user.ID
			filter.Plate = &plate
			filter.OwnerUserID = &ownerID
		} else if rawPlate != "" {
			filter.Plate = &rawPlate
		}

		subs, err := store.ListSubscriptions(r.Context(), deps.DB, filter)
		if err != nil {
			billingInternalError(deps, w, r, "abonelik listesi okunamadi", err)
			return
		}
		var next *int64
		if len(subs) > limit {
			nextID := subs[limit-1].ID
			next = &nextID
			subs = subs[:limit]
		}
		items := make([]subscriptionView, 0, len(subs))
		for _, sub := range subs {
			items = append(items, viewSubscription(sub))
		}
		WriteJSON(w, http.StatusOK, subscriptionsResponse{Items: items, NextAfterID: next})
	}
}

func cancelSubscriptionHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		subID, err := parseBillingID(chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_id", "invalid id")
			return
		}
		sub, err := store.GetSubscription(r.Context(), deps.DB, subID)
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		if !canManageSubscription(user, sub) {
			WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
			return
		}

		result, err := deps.Billing.Cancel(r.Context(), subID, time.Now())
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.subscription_cancelled",
			Entity:      "subscription",
			EntityID:    strconv.FormatInt(subID, 10),
			OldValue:    map[string]any{"status": string(sub.Status)},
			NewValue: map[string]any{
				"status":       string(billing.StatusCancelled),
				"credit_kurus": int64(result.Credit),
				"invoice_id":   result.InvoiceID,
			},
			RequestID: r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, cancelSubscriptionResponse{
			SubscriptionID: result.SubscriptionID,
			Status:         string(billing.StatusCancelled),
			Credit:         int64(result.Credit),
			InvoiceID:      result.InvoiceID,
		})
	}
}

func changePlanHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		subID, err := parseBillingID(chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_id", "invalid id")
			return
		}
		var req struct {
			PlanCode string `json:"plan_code"`
		}
		if err := decodeBillingJSON(w, r, &req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		req.PlanCode = strings.TrimSpace(req.PlanCode)
		if req.PlanCode == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "plan_code zorunlu")
			return
		}
		sub, err := store.GetSubscription(r.Context(), deps.DB, subID)
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		if !canManageSubscription(user, sub) {
			WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
			return
		}

		result, err := deps.Billing.ChangePlan(r.Context(), subID, req.PlanCode, time.Now())
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.plan_changed",
			Entity:      "subscription",
			EntityID:    strconv.FormatInt(subID, 10),
			OldValue:    map[string]any{"plan_code": sub.PlanCode},
			NewValue: map[string]any{
				"plan_code":    result.NewPlanCode,
				"net_kurus":    int64(result.Net),
				"charge_kurus": int64(result.Charge),
				"credit_kurus": int64(result.Credit),
			},
			RequestID: r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, changePlanResponse{
			SubscriptionID:  result.SubscriptionID,
			NewPlanCode:     result.NewPlanCode,
			Charge:          int64(result.Charge),
			Credit:          int64(result.Credit),
			Net:             int64(result.Net),
			Direction:       result.Direction,
			ChargeInvoiceID: result.ChargeInvoiceID,
			CreditInvoiceID: result.CreditInvoiceID,
		})
	}
}

func listInvoicesHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
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
		filter := store.InvoiceFilter{AfterID: afterID, Limit: limit + 1}
		if raw := r.URL.Query().Get("status"); raw != "" {
			if !validInvoiceStatus(raw) {
				WriteError(w, http.StatusBadRequest, "invalid_status", "invalid status")
				return
			}
			filter.Status = raw
		}
		if raw := r.URL.Query().Get("subscription_id"); raw != "" {
			subID, err := parseBillingID(raw)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid subscription_id")
				return
			}
			filter.SubscriptionID = &subID
		}
		if user.Role == auth.RoleDriver {
			if user.Plate == nil || *user.Plate == "" {
				WriteJSON(w, http.StatusOK, invoicesResponse{Items: []invoiceView{}})
				return
			}
			if filter.SubscriptionID != nil {
				sub, err := store.GetSubscription(r.Context(), deps.DB, *filter.SubscriptionID)
				if err != nil {
					writeBillingError(deps, w, r, err)
					return
				}
				if !canManageSubscription(user, sub) {
					WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
					return
				}
			}
			plate := *user.Plate
			ownerID := user.ID
			filter.Plate = &plate
			filter.OwnerUserID = &ownerID
		}

		invoices, err := store.ListInvoices(r.Context(), deps.DB, filter)
		if err != nil {
			billingInternalError(deps, w, r, "fatura listesi okunamadi", err)
			return
		}
		var next *int64
		if len(invoices) > limit {
			nextID := invoices[limit-1].ID
			next = &nextID
			invoices = invoices[:limit]
		}
		items := make([]invoiceView, 0, len(invoices))
		for _, inv := range invoices {
			items = append(items, viewInvoice(inv))
		}
		WriteJSON(w, http.StatusOK, invoicesResponse{Items: items, NextAfterID: next})
	}
}

func periodEndHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		var req periodEndRequest
		if err := decodeBillingJSON(w, r, &req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		periodStart, err := parseMonthStart(req.PeriodStart)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_period", "period_start YYYY-MM-01 olmali")
			return
		}
		created, err := deps.Billing.RunPeriodEnd(r.Context(), periodStart)
		if err != nil {
			billingInternalError(deps, w, r, "donem sonu calismadi", err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.period_end",
			Entity:      "billing_run",
			EntityID:    req.PeriodStart,
			NewValue:    map[string]any{"invoices_created": created},
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, periodEndResponse{InvoicesCreated: created})
	}
}

func paymentAttemptHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		invoiceID, err := parseBillingID(chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_id", "invalid id")
			return
		}
		var req paymentAttemptRequest
		if err := decodeBillingJSON(w, r, &req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		if req.Success == nil {
			WriteError(w, http.StatusBadRequest, "invalid_request", "success zorunlu")
			return
		}
		inv, err := deps.Billing.RecordPaymentAttempt(r.Context(), invoiceID, *req.Success, time.Now())
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		sub, err := store.GetSubscription(r.Context(), deps.DB, inv.SubscriptionID)
		if err != nil {
			billingInternalError(deps, w, r, "abonelik okunamadi", err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.payment_attempt",
			Entity:      "invoice",
			EntityID:    strconv.FormatInt(inv.ID, 10),
			NewValue: map[string]any{
				"success":             *req.Success,
				"invoice_status":      inv.Status,
				"attempts":            inv.Attempts,
				"subscription_status": string(sub.Status),
			},
			RequestID: r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, paymentAttemptResponse{
			InvoiceID:          inv.ID,
			SubscriptionID:     inv.SubscriptionID,
			Status:             inv.Status,
			Attempts:           inv.Attempts,
			NextRetryAt:        inv.NextRetryAt,
			SubscriptionStatus: string(sub.Status),
		})
	}
}

func reconciliationImportHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if deps.Billing == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		day, err := parseBillingDay(chi.URLParam(r, "day"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_day", "day YYYY-MM-DD olmali")
			return
		}
		var req []providerSettlementRequest
		if err := decodeBillingJSON(w, r, &req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		rows := make([]store.SettlementRow, 0, len(req))
		for _, row := range req {
			ref := strings.TrimSpace(row.ProviderRef)
			if ref == "" || row.Amount < 0 {
				WriteError(w, http.StatusBadRequest, "invalid_request", "gecersiz saglayici satiri")
				return
			}
			rows = append(rows, store.SettlementRow{ProviderRef: ref, Amount: tariff.Kurus(row.Amount)})
		}
		imported, err := deps.Billing.ImportProviderSettlement(r.Context(), day, rows)
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		report, err := deps.Billing.ReconcileDay(r.Context(), day)
		if err != nil {
			billingInternalError(deps, w, r, "mutabakat yapilamadi", err)
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "billing.reconciliation",
			Entity:      "reconciliation_report",
			EntityID:    chi.URLParam(r, "day"),
			NewValue: map[string]any{
				"imported":       imported,
				"our_total":      int64(report.OurTotal),
				"provider_total": int64(report.ProviderTotal),
				"diff":           int64(report.Diff),
			},
			RequestID: r.Header.Get("X-Request-ID"),
		}); err != nil {
			billingInternalError(deps, w, r, "denetim kaydi yazilamadi", err)
			return
		}
		WriteJSON(w, http.StatusOK, reconciliationImportResponse{Imported: imported, Report: report})
	}
}

func reconciliationReportHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day, err := parseBillingDay(chi.URLParam(r, "day"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_day", "day YYYY-MM-DD olmali")
			return
		}
		rec, err := store.GetReconciliationReport(r.Context(), deps.DB, day)
		if err != nil {
			writeBillingError(deps, w, r, err)
			return
		}
		var report billing.Report
		if err := json.Unmarshal(rec.Details, &report); err != nil {
			billingInternalError(deps, w, r, "mutabakat raporu cozulemedi", err)
			return
		}
		WriteJSON(w, http.StatusOK, report)
	}
}

func decodeBillingJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBillingBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func parseBillingID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("gecersiz id")
	}
	return id, nil
}

func parseMonthStart(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil || t.Day() != 1 {
		return time.Time{}, errors.New("gecersiz donem")
	}
	return t, nil
}

func parseBillingDay(raw string) (time.Time, error) {
	return time.Parse("2006-01-02", raw)
}

func validInvoiceStatus(status string) bool {
	switch status {
	case store.InvoiceStatusOpen, store.InvoiceStatusPaid, store.InvoiceStatusFailed, store.InvoiceStatusVoid:
		return true
	default:
		return false
	}
}

func canManageSubscription(user store.User, sub store.Subscription) bool {
	switch user.Role {
	case auth.RoleManager, auth.RoleAdmin:
		return true
	case auth.RoleDriver:
		if sub.UserID != nil && *sub.UserID == user.ID {
			return true
		}
		return user.Plate != nil && *user.Plate == sub.Plate
	default:
		return false
	}
}

func viewPlan(p store.Plan) planView {
	return planView{
		ID:      p.ID,
		Code:    p.Code,
		LotID:   p.LotID,
		Monthly: int64(p.MonthlyPrice),
		Active:  p.Active,
	}
}

func viewSubscription(sub store.Subscription) subscriptionView {
	return subscriptionView{
		ID:                 sub.ID,
		PlanID:             sub.PlanID,
		PlanCode:           sub.PlanCode,
		MonthlyPrice:       int64(sub.MonthlyPrice),
		LotID:              sub.LotID,
		Plate:              sub.Plate,
		UserID:             sub.UserID,
		Status:             string(sub.Status),
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		CancelledAt:        sub.CancelledAt,
		CreatedAt:          sub.CreatedAt,
		UpdatedAt:          sub.UpdatedAt,
	}
}

func viewInvoice(inv store.Invoice) invoiceView {
	return invoiceView{
		ID:             inv.ID,
		SubscriptionID: inv.SubscriptionID,
		PeriodStart:    inv.PeriodStart,
		PeriodEnd:      inv.PeriodEnd,
		Kind:           inv.Kind,
		Amount:         int64(inv.Amount),
		Status:         inv.Status,
		Attempts:       inv.Attempts,
		NextRetryAt:    inv.NextRetryAt,
		PaidAt:         inv.PaidAt,
		CreatedAt:      inv.CreatedAt,
	}
}

func writeBillingError(deps Deps, w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrPlanNotFound),
		errors.Is(err, store.ErrSubscriptionNotFound),
		errors.Is(err, store.ErrInvoiceNotFound),
		errors.Is(err, store.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrSubscriptionExists):
		WriteError(w, http.StatusConflict, "subscription_exists", "plate icin zaten aktif abonelik var")
	case errors.Is(err, store.ErrSubscriptionCancelled):
		WriteError(w, http.StatusConflict, "subscription_cancelled", "abonelik iptal edilmis")
	case errors.Is(err, store.ErrInvoiceAlreadyPaid):
		WriteError(w, http.StatusConflict, "invoice_paid", "fatura zaten odendi")
	case errors.Is(err, store.ErrProrationInvoiceClosed):
		WriteError(w, http.StatusConflict, "proration_invoice_closed",
			"bu donem icin odenmis gecis faturasi var; degisiklik sonraki doneme yapilmali")
	case errors.Is(err, billingsvc.ErrSamePlan):
		WriteError(w, http.StatusConflict, "same_plan", "abonelik zaten bu planda")
	case errors.Is(err, billingsvc.ErrInvalidPlan),
		errors.Is(err, billingsvc.ErrInvalidPlate),
		errors.Is(err, billingsvc.ErrInvalidAttempts),
		errors.Is(err, store.ErrInvalidSettlement):
		WriteError(w, http.StatusBadRequest, "invalid_request", "gecersiz istek")
	default:
		billingInternalError(deps, w, r, "faturalama islemi basarisiz", err)
	}
}

func billingInternalError(deps Deps, w http.ResponseWriter, r *http.Request, message string, err error) {
	if deps.Logger != nil {
		deps.Logger.Error(message, "hata", err, "request_id", r.Header.Get("X-Request-ID"))
	}
	WriteError(w, http.StatusInternalServerError, "internal", "internal error")
}
