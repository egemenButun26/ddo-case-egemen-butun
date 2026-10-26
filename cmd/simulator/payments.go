package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	kioskPaymentInterval = 10 * time.Second
	kioskRetryCooldown   = time.Minute
	kioskMaxBodyBytes    = 4096
)

type kioskQuote struct {
	SessionID int64 `json:"session_id"`
	DueKurus  int64 `json:"due_kurus"`
}

type kioskPaymentRequest struct {
	SessionID   int64 `json:"session_id"`
	AmountKurus int64 `json:"amount_kurus"`
}

type kioskCandidate struct {
	sessionID int64
	lotID     int64
	plate     string
}

type kioskPaymentLoop struct {
	apiURL    string
	cameraKey string
	client    *http.Client
	logger    *slog.Logger
	pool      *pgxpool.Pool
	seq       atomic.Int64
	cooldown  map[int64]time.Time
}

// runKioskPayments, acik oturumlar icin kiosk odemesi uretir. Boylece sağlayici
// simulatoru (gecikme, zaman asimi, gec onay) gercek HTTP akisinda calisir.
// Plaka olaylari deterministik kalir; odemeler calisan sistemin oturumlarindan
// turetilir.
func runKioskPayments(ctx context.Context, logger *slog.Logger, apiURL, cameraKey, databaseURL string) {
	if logger == nil {
		logger = slog.Default()
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		logger.Warn("simulator: kiosk odeme havuzu acilamadi", "hata", err)
		return
	}
	defer pool.Close()

	loop := &kioskPaymentLoop{
		apiURL:    apiURL,
		cameraKey: cameraKey,
		client:    &http.Client{Timeout: 20 * time.Second},
		logger:    logger,
		pool:      pool,
		cooldown:  make(map[int64]time.Time),
	}

	loop.tick(ctx)
	ticker := time.NewTicker(kioskPaymentInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			loop.tick(ctx)
		}
	}
}

func (l *kioskPaymentLoop) tick(ctx context.Context) {
	adaylar, err := l.candidates(ctx)
	if err != nil {
		l.logger.Warn("simulator: acik oturumlar okunamadi", "hata", err)
		return
	}
	for _, aday := range adaylar {
		if ctx.Err() != nil {
			return
		}
		if l.beklemede(aday.sessionID) {
			continue
		}
		l.isaretle(aday.sessionID)
		if l.ode(ctx, aday) {
			return
		}
	}
}

func (l *kioskPaymentLoop) candidates(ctx context.Context) ([]kioskCandidate, error) {
	rows, err := l.pool.Query(ctx, `
		SELECT s.id, s.lot_id, s.plate
		FROM "ya-sessions" s
		WHERE s.status = 'open'
		  AND s.entered_at <= now()
		  AND NOT EXISTS (
		    SELECT 1 FROM "ya-payments" p
		    WHERE p.session_id = s.id AND p.status IN ('pending', 'succeeded')
		  )
		ORDER BY s.id DESC
		LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []kioskCandidate
	for rows.Next() {
		var c kioskCandidate
		if err := rows.Scan(&c.sessionID, &c.lotID, &c.plate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (l *kioskPaymentLoop) beklemede(sessionID int64) bool {
	until, ok := l.cooldown[sessionID]
	return ok && time.Now().Before(until)
}

func (l *kioskPaymentLoop) isaretle(sessionID int64) {
	for id, until := range l.cooldown {
		if time.Now().After(until) {
			delete(l.cooldown, id)
		}
	}
	l.cooldown[sessionID] = time.Now().Add(kioskRetryCooldown)
}

func (l *kioskPaymentLoop) ode(ctx context.Context, aday kioskCandidate) bool {
	quoteURL := fmt.Sprintf("%s/kiosk/quote?lot_id=%d&plate=%s",
		l.apiURL, aday.lotID, url.QueryEscape(aday.plate))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Camera-Key", l.cameraKey)
	resp, err := l.client.Do(req)
	if err != nil {
		l.logger.Debug("simulator: kiosk teklifi alinamadi", "session_id", aday.sessionID, "hata", err)
		return false
	}
	var quote kioskQuote
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, kioskMaxBodyBytes)).Decode(&quote)
	drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK || decodeErr != nil {
		l.logger.Debug("simulator: kiosk teklifi yok",
			"session_id", aday.sessionID, "durum", resp.StatusCode)
		return false
	}
	if quote.SessionID <= 0 || quote.DueKurus <= 0 {
		return false
	}

	govde, err := json.Marshal(kioskPaymentRequest{
		SessionID:   quote.SessionID,
		AmountKurus: quote.DueKurus,
	})
	if err != nil {
		return false
	}
	anahtar := fmt.Sprintf("sim-pay-%d-%d", quote.SessionID, l.seq.Add(1))
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, l.apiURL+"/kiosk/payments", bytes.NewReader(govde))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Camera-Key", l.cameraKey)
	req.Header.Set("Idempotency-Key", anahtar)

	resp, err = l.client.Do(req)
	if err != nil {
		l.logger.Debug("simulator: kiosk odemesi gonderilemedi", "session_id", quote.SessionID, "hata", err)
		return false
	}
	drainAndClose(resp.Body)
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		l.logger.Info("simulator: kiosk odemesi olusturuldu",
			"session_id", quote.SessionID,
			"tutar_kurus", quote.DueKurus,
			"durum", resp.StatusCode,
		)
		return true
	case http.StatusConflict:
		l.logger.Debug("simulator: kiosk odemesi cakisti", "session_id", quote.SessionID)
	default:
		l.logger.Warn("simulator: kiosk odemesi reddedildi",
			"session_id", quote.SessionID, "durum", resp.StatusCode)
	}
	return false
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, kioskMaxBodyBytes))
	_ = body.Close()
}
