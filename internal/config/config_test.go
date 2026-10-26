package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

const (
	testCameraAPIKey     = "test-kamera-anahtari-0123456789abcdef"
	testProviderSecret   = "test-provider-secret-0123456789"
	testProviderURL      = "http://saglayici:9999"
	testPublicAPIURL     = "http://api-test:8080"
	testExpireInterval   = 7 * time.Second
	testProviderTimeoutS = "45s"
)

func env(values map[string]string) func(string) string {
	return func(key string) string {
		if v, ok := values[key]; ok {
			return v
		}
		if key == "PROVIDER_WEBHOOK_SECRET" {
			return testProviderSecret
		}
		return ""
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, beklenen :8080", cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, beklenen info", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, beklenen 10s", cfg.ShutdownTimeout)
	}
	if cfg.DBPingTimeout != 2*time.Second {
		t.Errorf("DBPingTimeout = %v, beklenen 2s", cfg.DBPingTimeout)
	}
	if cfg.AuthSessionTTL != 12*time.Hour {
		t.Errorf("AuthSessionTTL = %v, beklenen 12h", cfg.AuthSessionTTL)
	}
	if cfg.ProviderURL != defaultProviderURL {
		t.Errorf("ProviderURL = %q, beklenen %q", cfg.ProviderURL, defaultProviderURL)
	}
	if cfg.ProviderTimeout != defaultProviderTimeout {
		t.Errorf("ProviderTimeout = %v, beklenen %v", cfg.ProviderTimeout, defaultProviderTimeout)
	}
	if cfg.PaymentExpireInterval != defaultPaymentExpireInterval {
		t.Errorf("PaymentExpireInterval = %v, beklenen %v", cfg.PaymentExpireInterval, defaultPaymentExpireInterval)
	}
	if cfg.PublicAPIURL != defaultPublicAPIURL {
		t.Errorf("PublicAPIURL = %q, beklenen %q", cfg.PublicAPIURL, defaultPublicAPIURL)
	}
}

func TestLoadCustomValues(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":            "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY":          testCameraAPIKey,
		"HTTP_ADDR":               "127.0.0.1:9090",
		"LOG_LEVEL":               "debug",
		"SHUTDOWN_TIMEOUT":        "3s",
		"DB_PING_TIMEOUT":         "500ms",
		"AUTH_SESSION_TTL":        "45m",
		"PROVIDER_URL":            testProviderURL,
		"PROVIDER_TIMEOUT":        testProviderTimeoutS,
		"PAYMENT_EXPIRE_INTERVAL": "7s",
		"PUBLIC_API_URL":          testPublicAPIURL,
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("ShutdownTimeout = %v", cfg.ShutdownTimeout)
	}
	if cfg.DBPingTimeout != 500*time.Millisecond {
		t.Errorf("DBPingTimeout = %v", cfg.DBPingTimeout)
	}
	if cfg.AuthSessionTTL != 45*time.Minute {
		t.Errorf("AuthSessionTTL = %v", cfg.AuthSessionTTL)
	}
	if cfg.ProviderURL != testProviderURL {
		t.Errorf("ProviderURL = %q", cfg.ProviderURL)
	}
	if cfg.ProviderTimeout != 45*time.Second {
		t.Errorf("ProviderTimeout = %v", cfg.ProviderTimeout)
	}
	if cfg.PaymentExpireInterval != testExpireInterval {
		t.Errorf("PaymentExpireInterval = %v", cfg.PaymentExpireInterval)
	}
	if cfg.PublicAPIURL != testPublicAPIURL {
		t.Errorf("PublicAPIURL = %q", cfg.PublicAPIURL)
	}
}

func TestLoadRequiresProviderWebhookSecret(t *testing.T) {
	for _, deger := range []string{"", "kisa"} {
		_, err := Load(env(map[string]string{
			"DATABASE_URL":            "postgres://user:pass@localhost:5432/parkakil",
			"CAMERA_API_KEY":          testCameraAPIKey,
			"PROVIDER_WEBHOOK_SECRET": deger,
		}))
		if err == nil {
			t.Fatalf("%q icin hata bekleniyordu", deger)
		}
		if !strings.Contains(err.Error(), "PROVIDER_WEBHOOK_SECRET") {
			t.Errorf("hata degisken adini icermeli: %v", err)
		}
	}
}

func TestLoadRejectsInvalidProviderDurations(t *testing.T) {
	for _, ad := range []string{"PROVIDER_TIMEOUT", "PAYMENT_EXPIRE_INTERVAL"} {
		for _, raw := range []string{"abc", "0s", "-1s"} {
			t.Run(ad+"="+raw, func(t *testing.T) {
				_, err := Load(env(map[string]string{
					"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
					"CAMERA_API_KEY": testCameraAPIKey,
					ad:               raw,
				}))
				if err == nil {
					t.Fatalf("%q icin hata bekleniyordu", raw)
				}
				if !strings.Contains(err.Error(), ad) {
					t.Errorf("hata degisken adini icermeli: %v", err)
				}
			})
		}
	}
}

func TestErrorHidesProviderSecret(t *testing.T) {
	gizli := "gizli-saglayici-sirri-0123456789"
	_, err := Load(env(map[string]string{
		"DATABASE_URL":            "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY":          testCameraAPIKey,
		"PROVIDER_WEBHOOK_SECRET": gizli,
		"PROVIDER_TIMEOUT":        "abc",
	}))
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if strings.Contains(err.Error(), gizli) {
		t.Errorf("hata mesaji sirri icermemeli: %v", err)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	if _, err := Load(env(nil)); err == nil {
		t.Fatal("DATABASE_URL bosken hata bekleniyordu")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
		"LOG_LEVEL":      "verbose",
	}))
	if err == nil {
		t.Fatal("gecersiz LOG_LEVEL icin hata bekleniyordu")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Errorf("hata degisken adini icermeli: %v", err)
	}
}

func TestLoadRejectsInvalidDurations(t *testing.T) {
	for _, raw := range []string{"abc", "0s", "-1s"} {
		t.Run("SHUTDOWN_TIMEOUT="+raw, func(t *testing.T) {
			_, err := Load(env(map[string]string{
				"DATABASE_URL":     "postgres://user:pass@localhost:5432/parkakil",
				"CAMERA_API_KEY":   testCameraAPIKey,
				"SHUTDOWN_TIMEOUT": raw,
			}))
			if err == nil {
				t.Fatalf("%q icin hata bekleniyordu", raw)
			}
			if !strings.Contains(err.Error(), "SHUTDOWN_TIMEOUT") {
				t.Errorf("hata degisken adini icermeli: %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidDBPingTimeout(t *testing.T) {
	for _, raw := range []string{"abc", "0s", "-1s"} {
		t.Run("DB_PING_TIMEOUT="+raw, func(t *testing.T) {
			_, err := Load(env(map[string]string{
				"DATABASE_URL":    "postgres://user:pass@localhost:5432/parkakil",
				"CAMERA_API_KEY":  testCameraAPIKey,
				"DB_PING_TIMEOUT": raw,
			}))
			if err == nil {
				t.Fatalf("%q icin hata bekleniyordu", raw)
			}
		})
	}
}

func TestLoadRejectsInvalidAuthSessionTTL(t *testing.T) {
	for _, raw := range []string{"abc", "0s", "-1s"} {
		t.Run("AUTH_SESSION_TTL="+raw, func(t *testing.T) {
			_, err := Load(env(map[string]string{
				"DATABASE_URL":     "postgres://user:pass@localhost:5432/parkakil",
				"CAMERA_API_KEY":   testCameraAPIKey,
				"AUTH_SESSION_TTL": raw,
			}))
			if err == nil {
				t.Fatalf("%q icin hata bekleniyordu", raw)
			}
			if !strings.Contains(err.Error(), "AUTH_SESSION_TTL") {
				t.Errorf("hata degisken adini icermeli: %v", err)
			}
		})
	}
}

func TestErrorHidesDatabaseURL(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL":     "postgres://kullanici:gizliparola@localhost:5432/parkakil",
		"CAMERA_API_KEY":   testCameraAPIKey,
		"SHUTDOWN_TIMEOUT": "abc",
	}))
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if strings.Contains(err.Error(), "gizliparola") {
		t.Errorf("hata mesaji parolayi icermemeli: %v", err)
	}
}

func TestLoadRequiresCameraAPIKey(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/parkakil",
	}))
	if err == nil {
		t.Fatal("CAMERA_API_KEY bosken hata bekleniyordu")
	}
	if !strings.Contains(err.Error(), "CAMERA_API_KEY") {
		t.Errorf("hata degisken adini icermeli: %v", err)
	}
}

func TestLoadRejectsShortCameraAPIKey(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": "kisa",
	}))
	if err == nil {
		t.Fatal("kisa CAMERA_API_KEY icin hata bekleniyordu")
	}
	if !strings.Contains(err.Error(), "CAMERA_API_KEY") {
		t.Errorf("hata degisken adini icermeli: %v", err)
	}
}

func TestLoadCameraAPIKey(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CameraAPIKey != testCameraAPIKey {
		t.Errorf("CameraAPIKey = %q, beklenen test anahtari", cfg.CameraAPIKey)
	}
}

func TestErrorHidesCameraAPIKey(t *testing.T) {
	gizli := "gizli-anahtar-0123456789"
	_, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": gizli,
	}))
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if strings.Contains(err.Error(), gizli) {
		t.Errorf("hata mesaji anahtari icermemeli: %v", err)
	}
}

func TestLoadAllowsEmptyMetricsToken(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsToken != "" {
		t.Errorf("MetricsToken = %q, beklenen bos", cfg.MetricsToken)
	}
}

func TestLoadMetricsToken(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
		"METRICS_TOKEN":  "olcum-tokeni-0123456789",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsToken != "olcum-tokeni-0123456789" {
		t.Errorf("MetricsToken = %q", cfg.MetricsToken)
	}
}

func TestLoadRejectsShortMetricsToken(t *testing.T) {
	_, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://user:pass@localhost:5432/parkakil",
		"CAMERA_API_KEY": testCameraAPIKey,
		"METRICS_TOKEN":  "kisa",
	}))
	if err == nil {
		t.Fatal("kisa METRICS_TOKEN icin hata bekleniyordu")
	}
	if !strings.Contains(err.Error(), "METRICS_TOKEN") {
		t.Errorf("hata degisken adini icermeli: %v", err)
	}
}
