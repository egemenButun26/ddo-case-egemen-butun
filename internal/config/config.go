package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	CameraAPIKey    string
	MetricsToken    string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	DBPingTimeout   time.Duration
	AuthSessionTTL  time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:     getenv("DATABASE_URL"),
		HTTPAddr:        getenv("HTTP_ADDR"),
		CameraAPIKey:    getenv("CAMERA_API_KEY"),
		MetricsToken:    getenv("METRICS_TOKEN"),
		ShutdownTimeout: 10 * time.Second,
		DBPingTimeout:   2 * time.Second,
		AuthSessionTTL:  12 * time.Hour,
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
	if cfg.MetricsToken != "" && len(cfg.MetricsToken) < 16 {
		return Config{}, errors.New("config: METRICS_TOKEN en az 16 karakter olmali")
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
