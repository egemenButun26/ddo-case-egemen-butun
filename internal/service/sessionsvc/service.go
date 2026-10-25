// Package sessionsvc kamera olaylarini tek bir transaction icinde oturum
// kararlarina ve veritabani kayitlarina donusturur.
package sessionsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
)

type Service struct {
	pool *pgxpool.Pool
	cfg  session.Config
}

type Result struct {
	EventID            int64
	Outcome            session.Outcome
	SessionID          int64
	ReviewID           int64
	Replayed           bool
	AmountKurus        *tariff.Kurus
	ClosedByOrphanExit bool
}

func New(pool *pgxpool.Pool, cfg session.Config) (*Service, error) {
	if pool == nil {
		return nil, errors.New("sessionsvc: veritabani havuzu nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Service{pool: pool, cfg: cfg}, nil
}

func (s *Service) HandleEvent(ctx context.Context, ev session.Event) (Result, error) {
	if err := ev.Validate(); err != nil {
		return Result{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := store.LockLot(ctx, tx, ev.LotID); err != nil {
		return Result{}, err
	}

	mevcut, bulundu, err := store.FindEventByCameraKey(ctx, tx, ev.CameraID, ev.CameraEventID)
	if err != nil {
		return Result{}, err
	}
	if bulundu {
		reviewID, err := reviewIDForEvent(ctx, tx, mevcut.ID)
		if err != nil {
			return Result{}, err
		}
		res := Result{
			EventID:   mevcut.ID,
			Outcome:   mevcut.Outcome,
			SessionID: mevcut.SessionID,
			ReviewID:  reviewID,
			Replayed:  true,
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return res, nil
	}

	var res Result
	switch ev.Direction {
	case session.Entry:
		res, err = s.handleEntry(ctx, tx, ev)
	case session.Exit:
		res, err = s.handleExit(ctx, tx, ev)
	default:
		return Result{}, fmt.Errorf("%w: direction", session.ErrInvalidEvent)
	}
	if err != nil {
		return Result{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return res, nil
}

func (s *Service) handleEntry(ctx context.Context, tx pgx.Tx, ev session.Event) (Result, error) {
	open, err := store.ListOpenSessions(ctx, tx, ev.LotID)
	if err != nil {
		return Result{}, err
	}
	d, err := session.DecideEntry(s.cfg, ev, open)
	if err != nil {
		return Result{}, err
	}

	switch d.Outcome {
	case session.OutcomeOpened:
		v, err := store.TariffVersionAt(ctx, tx, ev.LotID, ev.CapturedAt)
		if errors.Is(err, store.ErrNoTariff) {
			return s.insertReview(ctx, tx, ev, d.Plate, session.Review{Reason: session.ReasonNoTariff})
		}
		if err != nil {
			return Result{}, err
		}

		eventID, err := store.InsertEvent(ctx, tx, ev, d.Plate, session.OutcomeOpened, 0)
		if err != nil {
			return Result{}, err
		}
		sessionID, err := store.InsertSession(ctx, tx, ev.LotID, d.Plate, eventID, ev.CapturedAt, v.ID)
		if err != nil {
			return Result{}, fmt.Errorf("oturum acilamadi: %w", err)
		}
		if err := store.SetEventOutcome(ctx, tx, eventID, session.OutcomeOpened, sessionID); err != nil {
			return Result{}, err
		}

		res := Result{EventID: eventID, Outcome: session.OutcomeOpened, SessionID: sessionID}

		orphans, err := store.ListOrphanExits(ctx, tx, ev.LotID, ev.CapturedAt)
		if err != nil {
			return Result{}, err
		}
		o, ok, err := session.MatchOrphanExit(s.cfg, d.Plate, ev.CapturedAt, orphans)
		if err != nil {
			return Result{}, err
		}
		if ok {
			amount, err := s.close(ctx, tx, sessionID, o.EventID, o.CapturedAt)
			if err != nil {
				return Result{}, err
			}
			if err := store.SetEventOutcome(ctx, tx, o.EventID, session.OutcomeClosed, sessionID); err != nil {
				return Result{}, err
			}
			res.ClosedByOrphanExit = true
			res.AmountKurus = &amount
		}
		return res, nil

	case session.OutcomeDuplicateEntry:
		eventID, err := store.InsertEvent(ctx, tx, ev, d.Plate, session.OutcomeDuplicateEntry, d.SessionID)
		if err != nil {
			return Result{}, err
		}
		return Result{EventID: eventID, Outcome: session.OutcomeDuplicateEntry, SessionID: d.SessionID}, nil

	case session.OutcomeReview:
		return s.insertReview(ctx, tx, ev, d.Plate, *d.Review)

	default:
		return Result{}, fmt.Errorf("sessionsvc: beklenmeyen giris karari %q", d.Outcome)
	}
}

func (s *Service) handleExit(ctx context.Context, tx pgx.Tx, ev session.Event) (Result, error) {
	open, err := store.ListOpenSessions(ctx, tx, ev.LotID)
	if err != nil {
		return Result{}, err
	}
	recent, err := store.ListRecentClosed(ctx, tx, ev.LotID, ev.CapturedAt, s.cfg.DuplicateWindow)
	if err != nil {
		return Result{}, err
	}
	d, err := session.DecideExit(s.cfg, ev, open, recent)
	if err != nil {
		return Result{}, err
	}

	switch d.Outcome {
	case session.OutcomeClosed:
		eventID, err := store.InsertEvent(ctx, tx, ev, d.Plate, session.OutcomeClosed, d.SessionID)
		if err != nil {
			return Result{}, err
		}
		amount, err := s.close(ctx, tx, d.SessionID, eventID, ev.CapturedAt)
		if err != nil {
			return Result{}, err
		}
		return Result{EventID: eventID, Outcome: session.OutcomeClosed, SessionID: d.SessionID, AmountKurus: &amount}, nil

	case session.OutcomeDuplicateExit:
		eventID, err := store.InsertEvent(ctx, tx, ev, d.Plate, session.OutcomeDuplicateExit, d.SessionID)
		if err != nil {
			return Result{}, err
		}
		return Result{EventID: eventID, Outcome: session.OutcomeDuplicateExit, SessionID: d.SessionID}, nil

	case session.OutcomeOrphanExit:
		eventID, err := store.InsertEvent(ctx, tx, ev, d.Plate, session.OutcomeOrphanExit, 0)
		if err != nil {
			return Result{}, err
		}
		return Result{EventID: eventID, Outcome: session.OutcomeOrphanExit}, nil

	case session.OutcomeReview:
		return s.insertReview(ctx, tx, ev, d.Plate, *d.Review)

	default:
		return Result{}, fmt.Errorf("sessionsvc: beklenmeyen cikis karari %q", d.Outcome)
	}
}

func (s *Service) insertReview(ctx context.Context, tx pgx.Tx, ev session.Event, plate string, r session.Review) (Result, error) {
	eventID, err := store.InsertEvent(ctx, tx, ev, plate, session.OutcomeReview, 0)
	if err != nil {
		return Result{}, err
	}
	reviewID, err := store.InsertReview(ctx, tx, eventID, ev.LotID, plate, r)
	if err != nil {
		return Result{}, err
	}
	return Result{EventID: eventID, Outcome: session.OutcomeReview, ReviewID: reviewID}, nil
}

func (s *Service) close(ctx context.Context, tx pgx.Tx, sessionID, exitEventID int64, exitedAt time.Time) (tariff.Kurus, error) {
	sess, err := store.GetSession(ctx, tx, sessionID)
	if err != nil {
		return 0, err
	}
	v, err := store.GetTariffVersion(ctx, tx, sess.TariffVersionID)
	if err != nil {
		return 0, err
	}
	q, err := tariff.Calculate(v, sess.EnteredAt, exitedAt)
	if err != nil {
		return 0, err
	}
	quote, err := json.Marshal(q)
	if err != nil {
		return 0, err
	}
	if err := store.CloseSession(ctx, tx, sess.ID, exitEventID, exitedAt, q.Total, quote); err != nil {
		return 0, err
	}
	return q.Total, nil
}

func reviewIDForEvent(ctx context.Context, tx pgx.Tx, eventID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM review_items WHERE event_id = $1`, eventID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}
