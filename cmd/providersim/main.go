// providersim, odeme saglayici simülatörüdür. Gelen tahsilat isteklerini
// tohumlu rastgele bir karara gore basarili, basarisiz, zaman asimi, cift
// bildirim ya da gec onay olarak yanitlar ve imzali bildirimi callback'e yollar.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const defaultAddr = ":8090"

type outcome string

const (
	outcomeSuccess outcome = "success"
	outcomeFailed  outcome = "failed"
	outcomeTimeout outcome = "timeout"
	outcomeDouble  outcome = "double"
	outcomeLate    outcome = "late"
)

type config struct {
	Addr       string
	Secret     string
	Seed       int64
	Timeout    time.Duration
	MinDelay   time.Duration
	MaxDelay   time.Duration
	HTTPClient *http.Client
}

type chargeRequest struct {
	ProviderRef string `json:"provider_ref"`
	AmountKurus int64  `json:"amount_kurus"`
	CallbackURL string `json:"callback_url"`
}

type notificationBody struct {
	ProviderRef string    `json:"provider_ref"`
	Outcome     string    `json:"outcome"`
	AmountKurus int64     `json:"amount_kurus"`
	ProviderAt  time.Time `json:"provider_at"`
}

type record struct {
	ProviderRef string    `json:"provider_ref"`
	AmountKurus int64     `json:"amount_kurus"`
	ProviderAt  time.Time `json:"provider_at"`
}

type simulator struct {
	cfg config

	mu       sync.Mutex
	rng      *rand.Rand
	records  map[string]record
	now      func() time.Time
	outcomeF func() outcome // testlerde karari sabitlemek icin
}

func newSimulator(cfg config) (*simulator, error) {
	if cfg.Secret == "" {
		return nil, errors.New("providersim: PROVIDER_WEBHOOK_SECRET zorunlu")
	}
	if len(cfg.Secret) < 16 {
		return nil, errors.New("providersim: PROVIDER_WEBHOOK_SECRET en az 16 karakter olmali")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("providersim: PROVIDER_TIMEOUT pozitif olmali")
	}
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.MinDelay < 0 || cfg.MaxDelay < cfg.MinDelay {
		return nil, errors.New("providersim: gecikme araligi gecersiz")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &simulator{
		cfg:     cfg,
		rng:     rand.New(rand.NewSource(cfg.Seed)),
		records: make(map[string]record),
		now:     func() time.Time { return time.Now().UTC() },
	}, nil
}

// decide, tohumlu rastgele sayidan karar uretir. Ayni tohum ayni diziyi verir.
func decide(r *rand.Rand) outcome {
	x := r.Float64()
	switch {
	case x < 0.70:
		return outcomeSuccess
	case x < 0.80:
		return outcomeFailed
	case x < 0.88:
		return outcomeTimeout
	case x < 0.95:
		return outcomeDouble
	default:
		return outcomeLate
	}
}

func (s *simulator) nextOutcome() outcome {
	if s.outcomeF != nil {
		return s.outcomeF()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return decide(s.rng)
}

func (s *simulator) nextDelay() time.Duration {
	if s.cfg.MaxDelay <= s.cfg.MinDelay {
		return s.cfg.MinDelay
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fark := s.cfg.MaxDelay - s.cfg.MinDelay
	return s.cfg.MinDelay + time.Duration(s.rng.Int63n(int64(fark)+1))
}

func (s *simulator) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /charges", s.handleCharge)
	mux.HandleFunc("GET /report", s.handleReport)
	return mux
}

func (s *simulator) handleCharge(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	var req chargeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if req.ProviderRef == "" || req.AmountKurus <= 0 || req.CallbackURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if _, err := http.NewRequest(http.MethodPost, req.CallbackURL, nil); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_callback_url"})
		return
	}

	karar := s.nextOutcome()
	if karar != outcomeTimeout {
		gecikme := s.nextDelay()
		go s.deliver(req, karar, gecikme)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "provider_ref": req.ProviderRef})
}

func (s *simulator) deliver(req chargeRequest, karar outcome, gecikme time.Duration) {
	switch karar {
	case outcomeLate:
		gecikme = s.cfg.Timeout + time.Second
	case outcomeSuccess, outcomeDouble, outcomeFailed:
	default:
		return
	}
	if gecikme > 0 {
		time.Sleep(gecikme)
	}

	at := s.now()
	switch karar {
	case outcomeSuccess:
		s.sendSuccess(req, at, 1)
	case outcomeDouble:
		s.sendSuccess(req, at, 2)
	case outcomeLate:
		s.sendSuccess(req, at, 1)
	case outcomeFailed:
		s.sendNotification(req, string(outcomeFailed), at)
	}
}

func (s *simulator) sendSuccess(req chargeRequest, at time.Time, adet int) {
	s.mu.Lock()
	if _, varMi := s.records[req.ProviderRef]; !varMi {
		s.records[req.ProviderRef] = record{
			ProviderRef: req.ProviderRef,
			AmountKurus: req.AmountKurus,
			ProviderAt:  at,
		}
	}
	s.mu.Unlock()
	for i := 0; i < adet; i++ {
		s.sendNotification(req, string(outcomeSuccess), at)
	}
}

func (s *simulator) sendNotification(req chargeRequest, sonuc string, at time.Time) {
	body, err := json.Marshal(notificationBody{
		ProviderRef: req.ProviderRef,
		Outcome:     sonuc,
		AmountKurus: req.AmountKurus,
		ProviderAt:  at,
	})
	if err != nil {
		return
	}
	hreq, err := http.NewRequest(http.MethodPost, req.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("X-Provider-Signature", signBody(s.cfg.Secret, body))
	resp, err := s.cfg.HTTPClient.Do(hreq)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func (s *simulator) handleReport(w http.ResponseWriter, r *http.Request) {
	gun := r.URL.Query().Get("day")
	if gun == "" {
		gun = s.now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", gun); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_day"})
		return
	}
	s.mu.Lock()
	items := make([]record, 0, len(s.records))
	for _, rec := range s.records {
		if rec.ProviderAt.Format("2006-01-02") == gun {
			items = append(items, rec)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, items)
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := getenvDefault("PROVIDERSIM_ADDR", defaultAddr)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return runHealthcheck(addr)
	}

	cfg, err := loadConfig(addr)
	if err != nil {
		logger.Error("yapilandirma hatasi", "hata", err)
		return 1
	}
	sim, err := newSimulator(cfg)
	if err != nil {
		logger.Error("simulator olusturulamadi", "hata", err)
		return 1
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           sim.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("saglayici simulatoru basladi", "adres", cfg.Addr)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("sunucu hatasi", "hata", err)
			return 1
		}
	case sig := <-sigCh:
		logger.Info("kapanis sinyali alindi", "sinyal", sig.String())
		ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
		defer iptal()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("duzgun kapanis basarisiz", "hata", err)
			_ = srv.Close()
			return 1
		}
	}
	logger.Info("kapandi")
	return 0
}

func loadConfig(addr string) (config, error) {
	seed := int64(4242)
	if raw := os.Getenv("PROVIDERSIM_SEED"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return config{}, fmt.Errorf("providersim: PROVIDERSIM_SEED gecersiz")
		}
		seed = v
	}
	timeout := 30 * time.Second
	if raw := os.Getenv("PROVIDER_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return config{}, fmt.Errorf("providersim: PROVIDER_TIMEOUT gecersiz")
		}
		timeout = d
	}
	return config{
		Addr:     addr,
		Secret:   os.Getenv("PROVIDER_WEBHOOK_SECRET"),
		Seed:     seed,
		Timeout:  timeout,
		MinDelay: 500 * time.Millisecond,
		MaxDelay: 3 * time.Second,
	}, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func runHealthcheck(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK {
		return 0
	}
	return 1
}
