package session

import (
	"fmt"
	"strconv"
	"time"

	"parkakil/internal/domain/plate"
)

type Outcome string

const (
	OutcomeOpened         Outcome = "opened"
	OutcomeDuplicateEntry Outcome = "duplicate_entry"
	OutcomeClosed         Outcome = "closed"
	OutcomeDuplicateExit  Outcome = "duplicate_exit"
	OutcomeOrphanExit     Outcome = "orphan_exit"
	OutcomeReview         Outcome = "review"
)

const (
	ReasonEmptyRead              = "empty_read"
	ReasonInvalidPlate           = "invalid_plate"
	ReasonPossibleDuplicateEntry = "possible_duplicate_entry"
	ReasonExitBeforeEntry        = "exit_before_entry"
	ReasonLowConfidenceReview    = "low_confidence_review"
	ReasonAmbiguous              = "ambiguous"
	ReasonInvalidFormat          = "invalid_format"
	ReasonAmbiguousExact         = "ambiguous_exact"
	ReasonNoTariff               = "no_tariff"
	ReasonLateEntry              = "late_entry"
	ReasonCapacityFull           = "capacity_full"
	ReasonCloseFailed            = "close_failed"
	ReasonBlacklisted            = "blacklisted"
)

type OpenSession struct {
	ID        int64
	Plate     string
	EnteredAt time.Time
}

type ClosedSpan struct {
	ID        int64
	Plate     string
	EnteredAt time.Time
	ExitedAt  time.Time
}

type ClosedSession struct {
	ID       int64
	Plate    string
	ExitedAt time.Time
}

type OrphanExit struct {
	EventID    int64
	Plate      string
	CapturedAt time.Time
}

type Review struct {
	Reason             string
	Confidence         int
	SuggestedSessionID int64
	RunnerUpSessionID  int64
	RunnerUpConfidence int
}

type Decision struct {
	Outcome   Outcome
	Plate     string
	SessionID int64
	Review    *Review
}

type Config struct {
	Thresholds      plate.Thresholds
	DuplicateWindow time.Duration
}

var DefaultConfig = Config{
	Thresholds:      plate.DefaultThresholds,
	DuplicateWindow: 5 * time.Minute,
}

func (c Config) Validate() error {
	if err := c.Thresholds.Validate(); err != nil {
		return fmt.Errorf("gecersiz ayar: %w", err)
	}
	if c.DuplicateWindow <= 0 {
		return fmt.Errorf("gecersiz ayar: duplicate window pozitif olmali")
	}
	return nil
}

func DecideEntry(c Config, ev Event, capacity int, open []OpenSession, closed []ClosedSpan) (Decision, error) {
	if capacity <= 0 {
		return Decision{}, fmt.Errorf("gecersiz kapasite: %d", capacity)
	}
	if err := c.Validate(); err != nil {
		return Decision{}, err
	}
	if err := ev.Validate(); err != nil {
		return Decision{}, err
	}
	if ev.Direction != Entry {
		return Decision{}, fmt.Errorf("%w: direction entry degil", ErrInvalidEvent)
	}

	p := plate.Normalize(ev.PlateRaw)
	if p == "" {
		return reviewDecision(ReasonEmptyRead, "", &Review{Reason: ReasonEmptyRead}), nil
	}
	if !plate.ValidTR(p) {
		return reviewDecision(ReasonInvalidPlate, p, &Review{Reason: ReasonInvalidPlate}), nil
	}

	if len(open) > 0 {
		res, err := plate.Match(p, openCandidates(open), c.Thresholds)
		if err != nil {
			return Decision{}, err
		}
		switch res.Decision {
		case plate.DecisionExact, plate.DecisionAuto:
			id := candidateID(res.Best)
			if sess, ok := findOpen(open, id); ok && ev.CapturedAt.Before(sess.EnteredAt.Add(-c.DuplicateWindow)) {
				return reviewDecision(ReasonLateEntry, p, &Review{Reason: ReasonLateEntry, SuggestedSessionID: id}), nil
			}
			return Decision{Outcome: OutcomeDuplicateEntry, Plate: p, SessionID: id}, nil
		case plate.DecisionReview:
			return reviewDecision(ReasonPossibleDuplicateEntry, p, reviewFromResult(ReasonPossibleDuplicateEntry, res)), nil
		}
	}

	if len(closed) > 0 {
		res, err := plate.Match(p, closedSpanCandidates(closed), c.Thresholds)
		if err != nil {
			return Decision{}, err
		}
		switch res.Decision {
		case plate.DecisionExact, plate.DecisionAuto:
			id := candidateID(res.Best)
			best, ok := findClosed(closed, id)
			if ok && ev.CapturedAt.Before(best.EnteredAt.Add(-c.DuplicateWindow)) {
				return reviewDecision(ReasonLateEntry, p, &Review{Reason: ReasonLateEntry, SuggestedSessionID: id}), nil
			}
			return Decision{Outcome: OutcomeDuplicateEntry, Plate: p, SessionID: id}, nil
		case plate.DecisionReview:
			return reviewDecision(ReasonPossibleDuplicateEntry, p, reviewFromResult(ReasonPossibleDuplicateEntry, res)), nil
		}
	}

	anahtar := plate.ConfusionKey(p)
	for _, s := range open {
		if plate.ConfusionKey(plate.Normalize(s.Plate)) == anahtar {
			return reviewDecision(ReasonPossibleDuplicateEntry, p, &Review{
				Reason:             ReasonPossibleDuplicateEntry,
				SuggestedSessionID: s.ID,
			}), nil
		}
	}

	if len(open) >= capacity {
		return reviewDecision(ReasonCapacityFull, p, &Review{Reason: ReasonCapacityFull}), nil
	}

	return Decision{Outcome: OutcomeOpened, Plate: p}, nil
}

func DecideExit(c Config, ev Event, open []OpenSession, recentClosed []ClosedSession) (Decision, error) {
	if err := c.Validate(); err != nil {
		return Decision{}, err
	}
	if err := ev.Validate(); err != nil {
		return Decision{}, err
	}
	if ev.Direction != Exit {
		return Decision{}, fmt.Errorf("%w: direction exit degil", ErrInvalidEvent)
	}

	p := plate.Normalize(ev.PlateRaw)
	if p == "" {
		return reviewDecision(ReasonEmptyRead, "", &Review{Reason: ReasonEmptyRead}), nil
	}

	if len(open) > 0 {
		res, err := plate.Match(p, openCandidates(open), c.Thresholds)
		if err != nil {
			return Decision{}, err
		}
		switch res.Decision {
		case plate.DecisionExact, plate.DecisionAuto:
			id := candidateID(res.Best)
			if sess, ok := findOpen(open, id); ok && sess.EnteredAt.After(ev.CapturedAt) {
				return reviewDecision(ReasonExitBeforeEntry, p, reviewFromResult(ReasonExitBeforeEntry, res)), nil
			}
			return Decision{Outcome: OutcomeClosed, Plate: p, SessionID: id}, nil
		case plate.DecisionReview:
			return reviewDecision(res.Reason, p, reviewFromResult(res.Reason, res)), nil
		}
	}

	if yakin := closedWithin(recentClosed, ev.CapturedAt, c.DuplicateWindow); len(yakin) > 0 {
		res, err := plate.Match(p, closedCandidates(yakin), c.Thresholds)
		if err != nil {
			return Decision{}, err
		}
		if res.Decision == plate.DecisionExact || res.Decision == plate.DecisionAuto {
			return Decision{Outcome: OutcomeDuplicateExit, Plate: p, SessionID: candidateID(res.Best)}, nil
		}
	}

	if !plate.ValidTR(p) {
		return reviewDecision(ReasonInvalidPlate, p, &Review{Reason: ReasonInvalidPlate}), nil
	}
	return Decision{Outcome: OutcomeOrphanExit, Plate: p}, nil
}

func MatchOrphanExit(c Config, sessionPlate string, enteredAt time.Time, orphans []OrphanExit) (OrphanExit, bool, error) {
	if err := c.Validate(); err != nil {
		return OrphanExit{}, false, err
	}

	adaylar := []plate.Candidate{{ID: "s", Plate: sessionPlate}}
	var enErken OrphanExit
	bulundu := false
	for _, o := range orphans {
		if o.CapturedAt.Before(enteredAt) {
			continue
		}
		res, err := plate.Match(o.Plate, adaylar, c.Thresholds)
		if err != nil {
			return OrphanExit{}, false, err
		}
		if res.Decision != plate.DecisionExact && res.Decision != plate.DecisionAuto {
			continue
		}
		if !bulundu || o.CapturedAt.Before(enErken.CapturedAt) ||
			(o.CapturedAt.Equal(enErken.CapturedAt) && o.EventID < enErken.EventID) {
			enErken = o
			bulundu = true
		}
	}
	return enErken, bulundu, nil
}

func reviewDecision(reason, p string, r *Review) Decision {
	if r == nil {
		r = &Review{Reason: reason}
	}
	return Decision{Outcome: OutcomeReview, Plate: p, Review: r}
}

func reviewFromResult(reason string, res plate.Result) *Review {
	return &Review{
		Reason:             reason,
		Confidence:         res.Confidence,
		SuggestedSessionID: candidateID(res.Best),
		RunnerUpSessionID:  candidateID(res.RunnerUp),
		RunnerUpConfidence: res.RunnerUpConfidence,
	}
}

func openCandidates(open []OpenSession) []plate.Candidate {
	out := make([]plate.Candidate, 0, len(open))
	for _, s := range open {
		out = append(out, plate.Candidate{ID: strconv.FormatInt(s.ID, 10), Plate: s.Plate})
	}
	return out
}

func closedCandidates(closed []ClosedSession) []plate.Candidate {
	out := make([]plate.Candidate, 0, len(closed))
	for _, s := range closed {
		out = append(out, plate.Candidate{ID: strconv.FormatInt(s.ID, 10), Plate: s.Plate})
	}
	return out
}

func closedSpanCandidates(closed []ClosedSpan) []plate.Candidate {
	out := make([]plate.Candidate, 0, len(closed))
	for _, s := range closed {
		out = append(out, plate.Candidate{ID: strconv.FormatInt(s.ID, 10), Plate: s.Plate})
	}
	return out
}

func candidateID(c *plate.Candidate) int64 {
	if c == nil {
		return 0
	}
	id, err := strconv.ParseInt(c.ID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func findOpen(open []OpenSession, id int64) (OpenSession, bool) {
	for _, s := range open {
		if s.ID == id {
			return s, true
		}
	}
	return OpenSession{}, false
}

func findClosed(closed []ClosedSpan, id int64) (ClosedSpan, bool) {
	for _, s := range closed {
		if s.ID == id {
			return s, true
		}
	}
	return ClosedSpan{}, false
}

func closedWithin(closed []ClosedSession, at time.Time, pencere time.Duration) []ClosedSession {
	out := make([]ClosedSession, 0, len(closed))
	for _, s := range closed {
		if absDuration(s.ExitedAt.Sub(at)) <= pencere {
			out = append(out, s)
		}
	}
	return out
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
