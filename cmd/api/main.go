package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/config"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/httpx"
	"parkakil/internal/service/billingsvc"
	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/service/paymentsvc"
	"parkakil/internal/service/sessionsvc"
)

func main() {
	os.Exit(run())
}

func run() int {
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		bootLogger.Error("yapilandirma hatasi", "hata", err)
		return 1
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return runHealthcheck(cfg)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("veritabani havuzu olusturulamadi")
		return 1
	}

	svc, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		logger.Error("oturum servisi olusturulamadi", "hata", err)
		pool.Close()
		return 1
	}

	payments, err := paymentsvc.New(pool, paymentsvc.Config{
		ProviderTimeout: cfg.ProviderTimeout,
		Thresholds:      plate.DefaultThresholds,
	})
	if err != nil {
		logger.Error("odeme servisi olusturulamadi", "hata", err)
		pool.Close()
		return 1
	}

	occupancySvc, err := occupancysvc.New(pool)
	if err != nil {
		logger.Error("doluluk servisi olusturulamadi", "hata", err)
		pool.Close()
		return 1
	}

	billingSvc, err := billingsvc.New(pool)
	if err != nil {
		logger.Error("faturalama servisi olusturulamadi", "hata", err)
		pool.Close()
		return 1
	}

	isCtx, isIptal := context.WithCancel(context.Background())
	defer isIptal()
	go paymentExpireLoop(isCtx, logger, payments, cfg.PaymentExpireInterval)
	dunningCtx, stopDunning := context.WithCancel(context.Background())
	defer stopDunning()
	go dunningLoop(dunningCtx, logger, billingSvc, cfg.DunningInterval)

	opsCtx, opsCancel := context.WithCancel(context.Background())
	var opsWG sync.WaitGroup
	opsWG.Add(2)
	go func() {
		defer opsWG.Done()
		occupancySvc.RunReleaseLoop(opsCtx, cfg.ReservationReleaseInterval, logger)
	}()
	go func() {
		defer opsWG.Done()
		occupancySvc.RunDriftLoop(opsCtx, cfg.DriftCheckInterval, logger)
	}()
	stopOps := func() {
		opsCancel()
		opsWG.Wait()
	}

	handler := httpx.NewRouter(httpx.Deps{
		Logger:        logger,
		Pinger:        pool,
		PingTimeout:   cfg.DBPingTimeout,
		Events:        svc,
		CameraKey:     cfg.CameraAPIKey,
		MetricsToken:  cfg.MetricsToken,
		DB:            pool,
		SessionTTL:    cfg.AuthSessionTTL,
		Occupancy:     occupancySvc,
		Payments:      payments,
		ProviderURL:   cfg.ProviderURL,
		WebhookSecret: cfg.ProviderWebhookSecret,
		PublicAPIURL:  cfg.PublicAPIURL,
		Billing:       billingSvc,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("sunucu basladi", "adres", cfg.HTTPAddr)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("sunucu hatasi", "hata", err)
			isIptal()
			stopDunning()
			stopOps()
			pool.Close()
			return 1
		}
	case sig := <-sigCh:
		logger.Info("kapanis sinyali alindi", "sinyal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("duzgun kapanis basarisiz", "hata", err)
			_ = srv.Close()
			stopOps()
			pool.Close()
			logger.Info("kapandi")
			return 1
		}
	}

	isIptal()
	stopDunning()
	stopOps()
	pool.Close()
	logger.Info("kapandi")
	return 0
}

// paymentExpireLoop, zaman asimi dolan bekleyen odemeleri duzenli araliklarla
// kapatir. Baglam iptal edilince durur.
func paymentExpireLoop(ctx context.Context, logger *slog.Logger, svc *paymentsvc.Service, aralik time.Duration) {
	ticker := time.NewTicker(aralik)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			iscCtx, iptal := context.WithTimeout(ctx, 10*time.Second)
			n, err := svc.ExpirePending(iscCtx, time.Now().UTC())
			iptal()
			if err != nil {
				logger.Error("bekleyen odemeler zaman asimina ugratilamadi", "hata", err)
				continue
			}
			if n > 0 {
				logger.Info("bekleyen odemeler zaman asimina ugratildi", "adet", n)
			}
		}
	}
}

func runHealthcheck(cfg config.Config) int {
	url, err := healthcheckURL(cfg.HTTPAddr)
	if err != nil {
		slog.Error("healthcheck adresi gecersiz", "hata", err)
		return 1
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
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

func healthcheckURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR ayristirilamadi: %w", err)
	}
	if port == "" {
		return "", errors.New("HTTP_ADDR port icermiyor")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/health", nil
}

// dunningLoop, vadesi gelen oturum sonu fatura tekrar denemelerini listeler.
// Gercek tahsilat bir saglayiciya baglanmaz; deneme sonucu
// POST /invoices/{id}/payment-attempts ucuyla kaydedilir.
func dunningLoop(ctx context.Context, logger *slog.Logger, svc *billingsvc.Service, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Info("dunning dongusu basladi", "aralik", interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			invoices, err := svc.DueRetries(ctx, time.Now())
			if err != nil {
				if ctx.Err() == nil {
					logger.Error("dunning listesi okunamadi", "hata", err)
				}
				continue
			}
			for _, inv := range invoices {
				logger.Info("dunning tekrar denemesi bekliyor",
					"fatura_id", inv.ID,
					"abonelik_id", inv.SubscriptionID,
					"deneme", inv.Attempts,
					"sonraki_deneme", inv.NextRetryAt,
				)
			}
		}
	}
}
