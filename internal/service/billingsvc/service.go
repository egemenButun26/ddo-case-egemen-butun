package billingsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
)

var (
	ErrInvalidPlan     = errors.New("gecersiz plan")
	ErrSamePlan        = errors.New("abonelik zaten bu planda")
	ErrInvalidPlate    = errors.New("gecersiz plaka")
	ErrInvalidAttempts = errors.New("gecersiz odeme denemesi")
)

type Service struct {
	pool *pgxpool.Pool
	loc  *time.Location
}

type Cancellation struct {
	SubscriptionID int64
	Credit         tariff.Kurus
	InvoiceID      int64
}

type ChangePlanResult struct {
	SubscriptionID  int64
	NewPlanCode     string
	Charge          tariff.Kurus
	Credit          tariff.Kurus
	Net             tariff.Kurus
	Direction       string
	ChargeInvoiceID int64
	CreditInvoiceID int64
}

func New(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, errors.New("billingsvc: veritabani havuzu nil")
	}
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return nil, fmt.Errorf("billingsvc: Europe/Istanbul yuklenemedi: %w", err)
	}
	return &Service{pool: pool, loc: loc}, nil
}

func (s *Service) monthBounds(t time.Time) (time.Time, time.Time) {
	l := t.In(s.loc)
	start := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, s.loc)
	return start, start.AddDate(0, 1, 0)
}

func (s *Service) localDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.loc)
}

func (s *Service) dayBounds(day time.Time) (start, end, date time.Time) {
	d := day.In(s.loc)
	start = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, s.loc)
	end = start.AddDate(0, 0, 1)
	date = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return start, end, date
}

func (s *Service) Subscribe(ctx context.Context, plate, planCode string, start time.Time) (store.Subscription, error) {
	return s.SubscribeForUser(ctx, plate, planCode, nil, start)
}

func (s *Service) SubscribeForUser(ctx context.Context, plate, planCode string, userID *int64, start time.Time) (store.Subscription, error) {
	if strings.TrimSpace(plate) == "" {
		return store.Subscription{}, ErrInvalidPlate
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Subscription{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	plan, err := store.GetPlanByCode(ctx, tx, planCode)
	if err != nil {
		return store.Subscription{}, err
	}
	if !plan.Active {
		return store.Subscription{}, ErrInvalidPlan
	}
	ps, pe := s.monthBounds(start)
	id, err := store.InsertSubscriptionWithUser(ctx, tx, plan.ID, plate, userID, ps, pe)
	if err != nil {
		return store.Subscription{}, err
	}
	sub, err := store.GetSubscription(ctx, tx, id)
	if err != nil {
		return store.Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Subscription{}, err
	}
	return sub, nil
}

func (s *Service) Cancel(ctx context.Context, subID int64, at time.Time) (Cancellation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Cancellation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sub, err := store.LockSubscription(ctx, tx, subID)
	if err != nil {
		return Cancellation{}, err
	}
	if sub.Status == billing.StatusCancelled {
		return Cancellation{}, store.ErrSubscriptionCancelled
	}
	ps := s.localDate(sub.CurrentPeriodStart)
	pe := s.localDate(sub.CurrentPeriodEnd)
	credit, err := billing.CancelCredit(sub.MonthlyPrice, ps, pe, at.In(s.loc))
	if err != nil {
		return Cancellation{}, err
	}

	result := Cancellation{SubscriptionID: sub.ID, Credit: credit}
	if credit > 0 {
		result.InvoiceID, err = store.AddProrationInvoice(ctx, tx, store.Invoice{
			SubscriptionID: sub.ID,
			PeriodStart:    ps,
			PeriodEnd:      pe,
			Kind:           store.InvoiceKindProrationCredit,
			Amount:         credit,
			Status:         store.InvoiceStatusOpen,
		})
		if err != nil {
			return Cancellation{}, err
		}
	}
	cancelledAt := at.In(s.loc)
	if err := store.SetSubscriptionStatus(ctx, tx, sub.ID, billing.StatusCancelled, &cancelledAt); err != nil {
		return Cancellation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Cancellation{}, err
	}
	return result, nil
}

func (s *Service) ChangePlan(ctx context.Context, subID int64, newPlanCode string, at time.Time) (ChangePlanResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChangePlanResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sub, err := store.LockSubscription(ctx, tx, subID)
	if err != nil {
		return ChangePlanResult{}, err
	}
	if sub.Status == billing.StatusCancelled {
		return ChangePlanResult{}, store.ErrSubscriptionCancelled
	}
	plan, err := store.GetPlanByCode(ctx, tx, newPlanCode)
	if err != nil {
		return ChangePlanResult{}, err
	}
	if !plan.Active {
		return ChangePlanResult{}, ErrInvalidPlan
	}
	if plan.ID == sub.PlanID {
		return ChangePlanResult{}, ErrSamePlan
	}

	ps := s.localDate(sub.CurrentPeriodStart)
	pe := s.localDate(sub.CurrentPeriodEnd)
	change, err := billing.ChangePlans(sub.MonthlyPrice, plan.MonthlyPrice, ps, pe, at.In(s.loc))
	if err != nil {
		return ChangePlanResult{}, err
	}

	result := ChangePlanResult{
		SubscriptionID: sub.ID,
		NewPlanCode:    plan.Code,
		Charge:         change.Charge,
		Credit:         change.Credit,
		Net:            change.Net,
		Direction:      change.Direction(),
	}
	if change.Charge > 0 {
		result.ChargeInvoiceID, err = store.AddProrationInvoice(ctx, tx, store.Invoice{
			SubscriptionID: sub.ID,
			PeriodStart:    ps,
			PeriodEnd:      pe,
			Kind:           store.InvoiceKindProrationCharge,
			Amount:         change.Charge,
			Status:         store.InvoiceStatusOpen,
		})
		if err != nil {
			return ChangePlanResult{}, err
		}
	}
	if change.Credit > 0 {
		result.CreditInvoiceID, err = store.AddProrationInvoice(ctx, tx, store.Invoice{
			SubscriptionID: sub.ID,
			PeriodStart:    ps,
			PeriodEnd:      pe,
			Kind:           store.InvoiceKindProrationCredit,
			Amount:         change.Credit,
			Status:         store.InvoiceStatusOpen,
		})
		if err != nil {
			return ChangePlanResult{}, err
		}
	}
	if err := store.SetSubscriptionPlan(ctx, tx, sub.ID, plan.ID); err != nil {
		return ChangePlanResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChangePlanResult{}, err
	}
	return result, nil
}

func (s *Service) RunPeriodEnd(ctx context.Context, periodStart time.Time) (int, error) {
	ps, pe := s.monthBounds(periodStart)
	nextEnd := pe.AddDate(0, 1, 0)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	runID, started, err := store.StartBillingRun(ctx, tx, ps)
	if err != nil {
		return 0, err
	}
	if !started {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}

	subs, err := store.ActiveSubscriptionsForPeriod(ctx, tx, ps)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, sub := range subs {
		inserted, err := store.InsertInvoiceIfAbsent(ctx, tx, store.Invoice{
			SubscriptionID: sub.ID,
			PeriodStart:    ps,
			PeriodEnd:      pe,
			Kind:           store.InvoiceKindPeriod,
			Amount:         sub.MonthlyPrice,
			Status:         store.InvoiceStatusOpen,
		})
		if err != nil {
			return 0, err
		}
		if inserted {
			created++
		}
		if err := store.SetSubscriptionPeriod(ctx, tx, sub.ID, pe, nextEnd); err != nil {
			return 0, err
		}
	}
	if err := store.FinishBillingRun(ctx, tx, runID, created); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func (s *Service) RecordPaymentAttempt(ctx context.Context, invoiceID int64, success bool, at time.Time) (store.Invoice, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Invoice{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inv, subStatus, err := store.LockInvoiceWithSubscription(ctx, tx, invoiceID)
	if err != nil {
		return store.Invoice{}, err
	}
	if inv.Status == store.InvoiceStatusPaid {
		return store.Invoice{}, store.ErrInvoiceAlreadyPaid
	}
	if inv.Status == store.InvoiceStatusVoid {
		return store.Invoice{}, ErrInvalidAttempts
	}
	at = at.In(s.loc)

	if success {
		if err := store.MarkInvoicePaid(ctx, tx, inv.ID, at); err != nil {
			return store.Invoice{}, err
		}
		if subStatus != billing.StatusCancelled {
			if err := store.SetSubscriptionStatus(ctx, tx, inv.SubscriptionID, billing.StatusActive, nil); err != nil {
				return store.Invoice{}, err
			}
		}
	} else {
		attempts := inv.Attempts + 1
		firstFailure := at
		if inv.Attempts > 0 && inv.NextRetryAt != nil && inv.Attempts <= len(billing.DunningSchedule) {
			firstFailure = inv.NextRetryAt.Add(-billing.DunningSchedule[inv.Attempts-1])
		}
		next, ok := billing.NextRetry(attempts, firstFailure)
		var nextPtr *time.Time
		if ok {
			nextPtr = &next
		}
		if err := store.MarkInvoiceFailed(ctx, tx, inv.ID, attempts, nextPtr); err != nil {
			return store.Invoice{}, err
		}
		if subStatus != billing.StatusCancelled {
			target := subStatus
			if attempts >= len(billing.DunningSchedule) {
				target = billing.StatusSuspended
			} else if subStatus == billing.StatusActive {
				target = billing.StatusPastDue
			}
			if err := store.SetSubscriptionStatus(ctx, tx, inv.SubscriptionID, target, nil); err != nil {
				return store.Invoice{}, err
			}
		}
	}

	updated, err := store.GetInvoice(ctx, tx, inv.ID)
	if err != nil {
		return store.Invoice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Invoice{}, err
	}
	return updated, nil
}

func (s *Service) DueRetries(ctx context.Context, now time.Time) ([]store.Invoice, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invoices, err := store.DueRetryInvoices(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return invoices, nil
}

func (s *Service) ImportProviderSettlement(ctx context.Context, day time.Time, rows []store.SettlementRow) (int, error) {
	_, _, date := s.dayBounds(day)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	count, err := store.UpsertProviderSettlements(ctx, tx, date, rows)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) ReconcileDay(ctx context.Context, day time.Time) (billing.Report, error) {
	start, end, date := s.dayBounds(day)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return billing.Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	payments, err := store.SucceededPaymentsOnDay(ctx, tx, start, end)
	if err != nil {
		return billing.Report{}, err
	}
	invoices, err := store.PaidInvoicesOnDay(ctx, tx, start, end)
	if err != nil {
		return billing.Report{}, err
	}
	provider, err := store.ProviderSettlementsOnDay(ctx, tx, date)
	if err != nil {
		return billing.Report{}, err
	}

	ours := make([]billing.Txn, 0, len(payments)+len(invoices))
	for _, p := range payments {
		ours = append(ours, billing.Txn{ProviderRef: p.ProviderRef, Amount: p.Amount})
	}
	for _, inv := range invoices {
		ours = append(ours, billing.Txn{
			ProviderRef: fmt.Sprintf("invoice:%d", inv.ID),
			Amount:      inv.Amount,
		})
	}
	theirs := make([]billing.Txn, 0, len(provider))
	for _, row := range provider {
		theirs = append(theirs, billing.Txn{ProviderRef: row.ProviderRef, Amount: row.Amount})
	}

	report := billing.Reconcile(ours, theirs)
	details, err := json.Marshal(report)
	if err != nil {
		return billing.Report{}, err
	}
	if err := store.SaveReconciliationReport(ctx, tx, date,
		report.OurTotal, report.ProviderTotal, report.Diff, details); err != nil {
		return billing.Report{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return billing.Report{}, err
	}
	return report, nil
}
