// Package paymentsvc odeme, iade ve kapi karari akislarini veritabani
// islemleriyle birlestirir. Is kurallari domain/payment paketindedir.
package paymentsvc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
)

var (
	// ErrNothingDue: oturumun odenmemis tutari yok.
	ErrNothingDue = errors.New("odenecek tutar yok")
	// ErrRefundExceedsPayment: iade toplami odeme tutarini asiyor.
	ErrRefundExceedsPayment = errors.New("iade tutari odemeyi asiyor")
	// ErrRefundNotSucceeded: yalniz basarili odeme iade edilebilir.
	ErrRefundNotSucceeded = errors.New("odeme basarili degil, iade edilemez")
)

// Config, servis ayarlari.
type Config struct {
	ProviderTimeout time.Duration
	Thresholds      plate.Thresholds
}

// Service odeme islemlerini yurutur.
type Service struct {
	pool *pgxpool.Pool
	cfg  Config
}

// New servisi kurar.
func New(pool *pgxpool.Pool, cfg Config) (*Service, error) {
	if pool == nil {
		return nil, errors.New("paymentsvc: veritabani havuzu nil")
	}
	if cfg.ProviderTimeout <= 0 {
		return nil, fmt.Errorf("paymentsvc: saglayici zaman asimi pozitif olmali (%s)", cfg.ProviderTimeout)
	}
	if err := cfg.Thresholds.Validate(); err != nil {
		return nil, fmt.Errorf("paymentsvc: %w", err)
	}
	return &Service{pool: pool, cfg: cfg}, nil
}

// Quote, oturumun o anki ucret dokumudur.
type Quote struct {
	SessionID       int64
	Total           tariff.Kurus
	Paid            tariff.Kurus
	Due             tariff.Kurus
	PermitExpiresAt time.Time // sifir = gecerli izin yok
}

// CreateReq odeme istegi.
type CreateReq struct {
	SessionID      int64
	Amount         tariff.Kurus
	IdempotencyKey string
	RequestHash    [32]byte
	ProviderRef    string
	Now            time.Time
}

// CreateResult odeme sonucu.
type CreateResult struct {
	PaymentID int64
	SessionID int64
	Payment   payment.Payment
	Replayed  bool
}

// ApplyResult bildirim sonucu.
type ApplyResult struct {
	PaymentID      int64
	SessionID      int64
	Payment        payment.Payment
	Transition     payment.Transition
	Settlement     payment.Settlement
	RefundRequired bool
	Overpaid       tariff.Kurus
}

// RefundReq iade istegi.
type RefundReq struct {
	PaymentID      int64
	Amount         tariff.Kurus
	Reason         string
	IdempotencyKey string
	RequestHash    [32]byte
	Now            time.Time
}

// RefundResult iade sonucu.
type RefundResult struct {
	Refund   store.StoredRefund
	Replayed bool
}

func truncMicro(t time.Time) time.Time {
	return t.Truncate(time.Microsecond)
}

func toDomain(items []store.StoredPayment) []payment.Payment {
	out := make([]payment.Payment, 0, len(items))
	for _, it := range items {
		out = append(out, it.Payment)
	}
	return out
}

func latestSucceeded(items []store.StoredPayment) (payment.Payment, bool) {
	var son payment.Payment
	var bulundu bool
	for _, it := range items {
		if it.Payment.Status != payment.StatusSucceeded {
			continue
		}
		if !bulundu || it.Payment.PaidAt.After(son.PaidAt) {
			son = it.Payment
			bulundu = true
		}
	}
	return son, bulundu
}

// quoteAt, oturumun verilen andaki toplamini hesaplar.
func quoteAt(version tariff.Version, entered, at time.Time) (tariff.Kurus, error) {
	q, err := tariff.Calculate(version, entered, at)
	if err != nil {
		return 0, err
	}
	return q.Total, nil
}

func openSession(sess store.StoredSession) error {
	if sess.Status != "open" {
		return store.ErrSessionNotOpen
	}
	return nil
}

// sessionSettlement oturumun toplamini ve odemelerini okur.
func (s *Service) sessionSettlement(ctx context.Context, q store.Querier, sess store.StoredSession, at time.Time) (payment.Settlement, []store.StoredPayment, error) {
	version, err := store.GetTariffVersion(ctx, q, sess.TariffVersionID)
	if err != nil {
		return payment.Settlement{}, nil, err
	}
	total, err := quoteAt(version, sess.EnteredAt, at)
	if err != nil {
		return payment.Settlement{}, nil, err
	}
	items, err := store.ListSessionPayments(ctx, q, sess.ID)
	if err != nil {
		return payment.Settlement{}, nil, err
	}
	st, err := payment.Settle(total, toDomain(items))
	if err != nil {
		return payment.Settlement{}, nil, err
	}
	return st, items, nil
}

// Quote oturumun o anki tutar, odenen ve kalan dokumunu doner.
func (s *Service) Quote(ctx context.Context, sessionID int64, now time.Time) (Quote, error) {
	if sessionID <= 0 {
		return Quote{}, errors.New("paymentsvc: oturum kimligi gecersiz")
	}
	now = truncMicro(now)
	sess, err := store.GetSession(ctx, s.pool, sessionID)
	if err != nil {
		return Quote{}, err
	}
	if err := openSession(sess); err != nil {
		return Quote{}, err
	}
	st, items, err := s.sessionSettlement(ctx, s.pool, sess, now)
	if err != nil {
		return Quote{}, err
	}
	out := Quote{SessionID: sessionID, Total: st.Total, Paid: st.Paid, Due: st.Due}
	if st.Due == 0 {
		if son, ok := latestSucceeded(items); ok {
			if izin := son.PaidAt.Add(payment.PermitWindow); now.Before(izin) {
				out.PermitExpiresAt = izin
			}
		}
	}
	return out, nil
}

// CreatePayment pending odeme yazar; ayni idempotency anahtariyla gelen istek
// ayni odemeyi tekrar doner.
func (s *Service) CreatePayment(ctx context.Context, req CreateReq) (CreateResult, error) {
	now := truncMicro(req.Now)
	if err := validateCreate(req, now); err != nil {
		return CreateResult{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CreateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mevcut, err := store.GetPaymentByIdempotencyKey(ctx, tx, req.IdempotencyKey)
	if err == nil {
		return replayPayment(mevcut, req.RequestHash)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return CreateResult{}, err
	}

	sess, err := store.GetSession(ctx, tx, req.SessionID)
	if err != nil {
		return CreateResult{}, err
	}
	if err := openSession(sess); err != nil {
		return CreateResult{}, err
	}

	st, _, err := s.sessionSettlement(ctx, tx, sess, now)
	if err != nil {
		return CreateResult{}, err
	}
	if st.Due == 0 {
		return CreateResult{}, ErrNothingDue
	}
	if req.Amount != st.Due {
		return CreateResult{}, fmt.Errorf("%w: odeme %v, kalan %v", payment.ErrAmountMismatch, req.Amount, st.Due)
	}

	p := payment.Payment{
		ProviderRef: req.ProviderRef,
		Amount:      req.Amount,
		Status:      payment.StatusPending,
		RequestedAt: now,
	}
	id, err := store.InsertPayment(ctx, tx, req.SessionID, req.IdempotencyKey, req.RequestHash, p)
	if err != nil {
		if isPaymentUniqueViolation(err) {
			// Benzersiz anahtar ihlali islemi iptal eder; mevcut satiri yeni
			// bir okumayla alip karsilastiriyoruz. Ayni istekte hem idempotency
			// anahtari hem saglayici referansi hem de bekleyen odeme kisiti
			// cakisabilir; once anahtarla eslesen odeme var mi diye bakilir.
			_ = tx.Rollback(ctx)
			mevcut, gerr := store.GetPaymentByIdempotencyKey(ctx, s.pool, req.IdempotencyKey)
			if gerr == nil {
				return replayPayment(mevcut, req.RequestHash)
			}
			if !errors.Is(gerr, store.ErrNotFound) {
				return CreateResult{}, gerr
			}
			return CreateResult{}, err
		}
		return CreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{PaymentID: id, SessionID: req.SessionID, Payment: p}, nil
}

func validateCreate(req CreateReq, now time.Time) error {
	if req.SessionID <= 0 {
		return errors.New("paymentsvc: oturum kimligi gecersiz")
	}
	if req.Amount <= 0 {
		return fmt.Errorf("%w: odeme tutari pozitif olmali", payment.ErrInvalidAmount)
	}
	if req.ProviderRef == "" {
		return fmt.Errorf("%w: saglayici referansi bos", payment.ErrInvalidPayment)
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 255 {
		return fmt.Errorf("%w: idempotency anahtari gecersiz", payment.ErrInvalidPayment)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: istek zamani bos", payment.ErrInvalidPayment)
	}
	return nil
}

func isPaymentUniqueViolation(err error) bool {
	return errors.Is(err, store.ErrIdempotencyConflict) ||
		errors.Is(err, store.ErrProviderRefTaken) ||
		errors.Is(err, store.ErrPendingPaymentExists)
}

func replayPayment(sp store.StoredPayment, hash [32]byte) (CreateResult, error) {
	if sp.RequestHash != hash {
		return CreateResult{}, fmt.Errorf("%w: %s", store.ErrIdempotencyConflict, sp.IdempotencyKey)
	}
	return CreateResult{
		PaymentID: sp.ID,
		SessionID: sp.SessionID,
		Payment:   sp.Payment,
		Replayed:  true,
	}, nil
}

// ApplyNotification saglayici bildirimini uygular ve basarida fazla odeme
// olup olmadigini bildirir. Iade kaydi acmaz.
func (s *Service) ApplyNotification(ctx context.Context, n payment.Notification) (ApplyResult, error) {
	n.ProviderAt = truncMicro(n.ProviderAt)
	n.ReceivedAt = truncMicro(n.ReceivedAt)
	if n.ProviderRef == "" {
		return ApplyResult{}, fmt.Errorf("%w: saglayici referansi bos", payment.ErrInvalidNotification)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sp, err := store.LockPaymentByProviderRef(ctx, tx, n.ProviderRef)
	if err != nil {
		return ApplyResult{}, err
	}

	p, tr, err := payment.Apply(sp.Payment, n)
	if err != nil {
		return ApplyResult{}, err
	}
	if !tr.Duplicate {
		if err := store.UpdatePaymentStatus(ctx, tx, sp.ID, p, tr.LateApproval); err != nil {
			return ApplyResult{}, err
		}
	}

	res := ApplyResult{
		PaymentID:  sp.ID,
		SessionID:  sp.SessionID,
		Payment:    p,
		Transition: tr,
	}
	if p.Status == payment.StatusSucceeded {
		sess, err := store.GetSession(ctx, tx, sp.SessionID)
		if err != nil {
			return ApplyResult{}, err
		}
		st, _, err := s.sessionSettlement(ctx, tx, sess, p.PaidAt)
		if err != nil {
			return ApplyResult{}, err
		}
		res.Settlement = st
		res.RefundRequired = st.RefundRequired()
		res.Overpaid = st.Overpaid
	}

	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, err
	}
	return res, nil
}

// ExpirePending zaman asimi dolan bekleyen odemeleri timed_out yapar.
func (s *Service) ExpirePending(ctx context.Context, now time.Time) (int, error) {
	now = truncMicro(now)
	cutoff := now.Add(-s.cfg.ProviderTimeout)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sayac := 0
	for {
		items, err := store.ListExpiredPending(ctx, tx, cutoff, 100)
		if err != nil {
			return 0, err
		}
		if len(items) == 0 {
			break
		}
		degisen := 0
		for _, sp := range items {
			p, changed, err := payment.Expire(sp.Payment, now, s.cfg.ProviderTimeout)
			if err != nil {
				return 0, err
			}
			if !changed {
				continue
			}
			if err := store.UpdatePaymentStatus(ctx, tx, sp.ID, p, false); err != nil {
				return 0, err
			}
			sayac++
			degisen++
		}
		if len(items) < 100 || degisen == 0 {
			break
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return sayac, nil
}

// DecideGate kapi kararini salt okuma ile uretir.
func (s *Service) DecideGate(ctx context.Context, lotID int64, plateRaw string, now time.Time) (payment.GateDecision, int64, error) {
	now = truncMicro(now)
	open, err := store.ListOpenSessions(ctx, s.pool, lotID)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}

	adaylar := make([]plate.Candidate, 0, len(open))
	for _, o := range open {
		adaylar = append(adaylar, plate.Candidate{ID: strconv.FormatInt(o.ID, 10), Plate: o.Plate})
	}
	m, err := plate.Match(plateRaw, adaylar, s.cfg.Thresholds)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}
	if m.Decision == plate.DecisionNone || m.Best == nil {
		d, err := payment.DecideGate(payment.GateInput{Match: plate.DecisionNone, Found: false})
		return d, 0, err
	}

	sessionID, err := strconv.ParseInt(m.Best.ID, 10, 64)
	if err != nil {
		return payment.GateDecision{}, 0, fmt.Errorf("paymentsvc: oturum kimligi cozulemedi: %w", err)
	}
	sess, err := store.GetSession(ctx, s.pool, sessionID)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}
	if err := openSession(sess); err != nil {
		return payment.GateDecision{}, 0, err
	}

	items, err := store.ListSessionPayments(ctx, s.pool, sessionID)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}
	version, err := store.GetTariffVersion(ctx, s.pool, sess.TariffVersionID)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}
	prior, err := store.SumOpenDebtByPlate(ctx, s.pool, plate.Normalize(plateRaw))
	if err != nil {
		return payment.GateDecision{}, 0, err
	}

	in := payment.GateInput{
		Match:     m.Decision,
		Found:     true,
		EnteredAt: sess.EnteredAt,
		Now:       now,
		Quote: func(at time.Time) (tariff.Kurus, error) {
			return quoteAt(version, sess.EnteredAt, at)
		},
		Payments:  toDomain(items),
		PriorDebt: prior,
	}
	d, err := payment.DecideGate(in)
	if err != nil {
		return payment.GateDecision{}, 0, err
	}
	return d, sessionID, nil
}

// CreateRefund bekleyen iade kaydi acar; ayni anahtar ayni iadeyi doner.
func (s *Service) CreateRefund(ctx context.Context, req RefundReq) (RefundResult, error) {
	now := truncMicro(req.Now)
	if req.PaymentID <= 0 {
		return RefundResult{}, errors.New("paymentsvc: odeme kimligi gecersiz")
	}
	if req.Amount <= 0 {
		return RefundResult{}, fmt.Errorf("%w: iade tutari pozitif olmali", tariff.ErrNegativeAmount)
	}
	if req.Reason != "overpaid" && req.Reason != "manual" {
		return RefundResult{}, fmt.Errorf("paymentsvc: gecersiz iade nedeni %q", req.Reason)
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 255 {
		return RefundResult{}, errors.New("paymentsvc: idempotency anahtari gecersiz")
	}
	if now.IsZero() {
		return RefundResult{}, errors.New("paymentsvc: istek zamani bos")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return RefundResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mevcut, err := store.GetRefundByIdempotencyKey(ctx, tx, req.IdempotencyKey)
	if err == nil {
		if mevcut.RequestHash != req.RequestHash {
			return RefundResult{}, fmt.Errorf("%w: %s", store.ErrIdempotencyConflict, req.IdempotencyKey)
		}
		return RefundResult{Refund: mevcut, Replayed: true}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return RefundResult{}, err
	}

	sp, err := store.LockPaymentByID(ctx, tx, req.PaymentID)
	if err != nil {
		return RefundResult{}, err
	}
	if sp.Payment.Status != payment.StatusSucceeded {
		return RefundResult{}, ErrRefundNotSucceeded
	}
	toplam, err := store.SumRefunds(ctx, tx, sp.ID)
	if err != nil {
		return RefundResult{}, err
	}
	if toplam+req.Amount > sp.Payment.Amount {
		return RefundResult{}, ErrRefundExceedsPayment
	}

	id, err := store.InsertRefund(ctx, tx, sp.ID, req.Amount, req.Reason, req.IdempotencyKey,
		req.RequestHash, "", now)
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) {
			_ = tx.Rollback(ctx)
			mevcut, gerr := store.GetRefundByIdempotencyKey(ctx, s.pool, req.IdempotencyKey)
			if gerr != nil {
				return RefundResult{}, gerr
			}
			if mevcut.RequestHash != req.RequestHash {
				return RefundResult{}, fmt.Errorf("%w: %s", store.ErrIdempotencyConflict, req.IdempotencyKey)
			}
			return RefundResult{Refund: mevcut, Replayed: true}, nil
		}
		return RefundResult{}, err
	}

	r := store.StoredRefund{
		ID:             id,
		PaymentID:      sp.ID,
		Amount:         req.Amount,
		Status:         "pending",
		Reason:         req.Reason,
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    req.RequestHash,
		RequestedAt:    now,
	}
	if err := tx.Commit(ctx); err != nil {
		return RefundResult{}, err
	}
	return RefundResult{Refund: r}, nil
}
