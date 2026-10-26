package session

import (
	"errors"
	"fmt"
	"time"
)

type Direction string

const (
	Entry Direction = "entry"
	Exit  Direction = "exit"
)

type Event struct {
	LotID         int64
	CameraID      string
	CameraEventID string
	Direction     Direction
	PlateRaw      string
	CapturedAt    time.Time
}

var ErrInvalidEvent = errors.New("gecersiz kamera olayi")

func (e Event) Validate() error {
	if e.LotID <= 0 {
		return fmt.Errorf("%w: lot_id", ErrInvalidEvent)
	}
	if e.CameraID == "" {
		return fmt.Errorf("%w: camera_id bos", ErrInvalidEvent)
	}
	if len(e.CameraID) > 128 {
		return fmt.Errorf("%w: camera_id 128 bayttan uzun", ErrInvalidEvent)
	}
	if e.CameraEventID == "" {
		return fmt.Errorf("%w: camera_event_id bos", ErrInvalidEvent)
	}
	if len(e.CameraEventID) > 128 {
		return fmt.Errorf("%w: camera_event_id 128 bayttan uzun", ErrInvalidEvent)
	}
	if e.Direction != Entry && e.Direction != Exit {
		return fmt.Errorf("%w: direction", ErrInvalidEvent)
	}
	if e.CapturedAt.IsZero() {
		return fmt.Errorf("%w: captured_at", ErrInvalidEvent)
	}
	if len(e.PlateRaw) > 64 {
		return fmt.Errorf("%w: plate_raw 64 bayttan uzun", ErrInvalidEvent)
	}
	return nil
}
