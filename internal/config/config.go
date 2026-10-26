package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Config struct {
	DatabaseURL           string
	HTTPAddr              string
	CameraAPIKey          string
	LogLevel              slog.Level
	ShutdownTimeout       time.Duration
	DBPingTimeout         time.Duration
	AuthSessionTTL        time.Duration
	ProviderURL           string
	ProviderWebhookSecret string
	ProviderTimeout       time.Duration
	PaymentExpireInterval time.Duration
	PublicAPIURL          string
}

const (
	defaultProviderURL           = "http://providersim:8090"
	defaultProviderTimeout       = 30 * time.Second
	defaultPaymentExpireInterval = 10 * time.Second
	defaultPublicAPIURL          = "http://api:8080"
)

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:           getenv("DATABASE_URL"),
		HTTPAddr:              getenv("HTTP_ADDR"),
		CameraAPIKey:          getenv("CAMERA_API_KEY"),
		ShutdownTimeout:       10 * time.Second,
		DBPingTimeout:         2 * time.Second,
		AuthSessionTTL:        12 * time.Hour,
		ProviderURL:           getenv("PROVIDER_URL"),
		ProviderWebhookSecret: getenv("PROVIDER_WEBHOOK_SECRET"),
		ProviderTimeout:       defaultProviderTimeout,
		PaymentExpireInterval: defaultPaymentExpireInterval,
		PublicAPIURL:          getenv("PUBLIC_API_URL"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL zorunlu")
	}
	if cfg.CameraAPIKey == "" {
		return Config{}, errors.New("config: CAMERA_API_KEY zorunlu")
	}
	if len(cfg.CameraAPIKey) < 32 {
		return Config{}, errors.New("config: CAMERA_API_KEY en az 32 karakter olmali")
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	level, err := parseLogLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	cfg.ShutdownTimeout, err = parsePositiveDuration("SHUTDOWN_TIMEOUT", getenv("SHUTDOWN_TIMEOUT"), cfg.ShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.DBPingTimeout, err = parsePositiveDuration("DB_PING_TIMEOUT", getenv("DB_PING_TIMEOUT"), cfg.DBPingTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.AuthSessionTTL, err = parsePositiveDuration("AUTH_SESSION_TTL", getenv("AUTH_SESSION_TTL"), cfg.AuthSessionTTL)
	if err != nil {
		return Config{}, err
	}

	if cfg.ProviderWebhookSecret == "" {
		return Config{}, errors.New("config: PROVIDER_WEBHOOK_SECRET zorunlu")
	}
	if len(cfg.ProviderWebhookSecret) < 16 {
		return Config{}, errors.New("config: PROVIDER_WEBHOOK_SECRET en az 16 karakter olmali")
	}
	if cfg.ProviderURL == "" {
		cfg.ProviderURL = defaultProviderURL
	}
	if cfg.PublicAPIURL == "" {
		cfg.PublicAPIURL = defaultPublicAPIURL
	}
	cfg.ProviderTimeout, err = parsePositiveDuration("PROVIDER_TIMEOUT", getenv("PROVIDER_TIMEOUT"), cfg.ProviderTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.PaymentExpireInterval, err = parsePositiveDuration("PAYMENT_EXPIRE_INTERVAL", getenv("PAYMENT_EXPIRE_INTERVAL"), cfg.PaymentExpireInterval)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch raw {
	case "":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: LOG_LEVEL gecersiz (debug|info|warn|error)")
	}
}

func parsePositiveDuration(name, raw string, def time.Duration) (time.Duration, error) {
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s gecersiz sure bicimi", name)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: %s pozitif olmali", name)
	}
	return d, nil
}
