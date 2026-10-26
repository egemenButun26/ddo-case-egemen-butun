package billing

import "errors"

type Status string

const (
	StatusActive    Status = "active"
	StatusPastDue   Status = "past_due"
	StatusSuspended Status = "suspended"
	StatusCancelled Status = "cancelled"
)

var (
	ErrInvalidStatus     = errors.New("gecersiz abonelik durumu")
	ErrInvalidTransition = errors.New("gecersiz abonelik durum gecisi")
)

var transitions = map[Status][]Status{
	StatusActive:    {StatusActive, StatusPastDue, StatusSuspended, StatusCancelled},
	StatusPastDue:   {StatusPastDue, StatusActive, StatusSuspended, StatusCancelled},
	StatusSuspended: {StatusSuspended, StatusActive, StatusCancelled},
	StatusCancelled: {StatusCancelled},
}

func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

func Transition(from, to Status) (Status, error) {
	if !from.Valid() || !to.Valid() {
		return "", ErrInvalidStatus
	}
	for _, next := range transitions[from] {
		if next == to {
			return to, nil
		}
	}
	return from, ErrInvalidTransition
}
