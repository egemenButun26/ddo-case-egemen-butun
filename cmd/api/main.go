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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/config"
	"parkakil/internal/domain/session"
	"parkakil/internal/httpx"
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

	handler := httpx.NewRouter(httpx.Deps{
		Logger:      logger,
		Pinger:      pool,
		PingTimeout: cfg.DBPingTimeout,
		Events:      svc,
		CameraKey:   cfg.CameraAPIKey,
		DB:          pool,
		SessionTTL:  cfg.AuthSessionTTL,
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
			pool.Close()
			logger.Info("kapandi")
			return 1
		}
	}

	pool.Close()
	logger.Info("kapandi")
	return 0
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
