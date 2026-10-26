package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"parkakil/internal/simulator"
)

type counters struct {
	sent        atomic.Int64
	created     atomic.Int64
	replayed    atomic.Int64
	conflict    atomic.Int64
	clientError atomic.Int64
	failed      atomic.Int64
}

type cameraEventRequest struct {
	LotID         int64               `json:"lot_id"`
	CameraID      string              `json:"camera_id"`
	CameraEventID string              `json:"camera_event_id"`
	Direction     simulator.Direction `json:"direction"`
	PlateRaw      string              `json:"plate_raw"`
	CapturedAt    time.Time           `json:"captured_at"`
}

type sender struct {
	client      *http.Client
	apiURL      string
	cameraKey   string
	logger      *slog.Logger
	counters    *counters
	backoff     time.Duration
	maxAttempts int
}

func newSender(apiURL, cameraKey string, logger *slog.Logger, counter *counters) *sender {
	if logger == nil {
		logger = slog.Default()
	}
	if counter == nil {
		counter = &counters{}
	}
	return &sender{
		client:      &http.Client{Timeout: 10 * time.Second},
		apiURL:      strings.TrimRight(apiURL, "/"),
		cameraKey:   cameraKey,
		logger:      logger,
		counters:    counter,
		backoff:     500 * time.Millisecond,
		maxAttempts: 6,
	}
}

func (s *sender) send(ctx context.Context, ev simulator.Event) error {
	s.counters.sent.Add(1)

	govde, err := json.Marshal(cameraEventRequest{
		LotID:         ev.LotID,
		CameraID:      ev.CameraID,
		CameraEventID: ev.CameraEventID,
		Direction:     ev.Direction,
		PlateRaw:      ev.PlateRaw,
		CapturedAt:    ev.CapturedAt,
	})
	if err != nil {
		s.counters.failed.Add(1)
		return err
	}

	var sonHata error
	for deneme := 0; deneme < s.maxAttempts; deneme++ {
		if deneme > 0 {
			if err := s.wait(ctx, s.backoff<<uint(deneme-1)); err != nil {
				s.counters.failed.Add(1)
				return err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.apiURL+"/camera-events", bytes.NewReader(govde))
		if err != nil {
			s.counters.failed.Add(1)
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Camera-Key", s.cameraKey)

		resp, err := s.client.Do(req)
		if err != nil {
			sonHata = err
			if ctx.Err() != nil {
				s.counters.failed.Add(1)
				return ctx.Err()
			}
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusCreated:
			s.counters.created.Add(1)
			return nil
		case resp.StatusCode == http.StatusOK:
			s.counters.replayed.Add(1)
			return nil
		case resp.StatusCode == http.StatusConflict:
			s.counters.conflict.Add(1)
			s.logger.Info("olay cakisti",
				"durum", resp.StatusCode,
				"camera_event_id", ev.CameraEventID,
			)
			return nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			s.counters.clientError.Add(1)
			s.logger.Warn("olay reddedildi",
				"durum", resp.StatusCode,
				"camera_event_id", ev.CameraEventID,
			)
			return nil
		default:
			sonHata = fmt.Errorf("beklenmeyen durum kodu: %d", resp.StatusCode)
			continue
		}
	}

	if sonHata == nil {
		sonHata = fmt.Errorf("gonderim basarisiz")
	}
	s.counters.failed.Add(1)
	return sonHata
}

func (s *sender) wait(ctx context.Context, sure time.Duration) error {
	timer := time.NewTimer(sure)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
