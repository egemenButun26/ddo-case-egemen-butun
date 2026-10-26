package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type healthBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func HealthHandler(p Pinger, timeout time.Duration) http.HandlerFunc {
	return healthHandler(p, timeout, slog.Default())
}

func hzHandler(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func healthHandler(p Pinger, timeout time.Duration, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		result := make(chan error, 1)
		go func() {
			result <- p.Ping(ctx)
		}()

		select {
		case err := <-result:
			if err != nil {
				logger.Warn("veritabani ping hatasi", "hata", err)
				WriteError(w, http.StatusServiceUnavailable, "unavailable", "database unreachable")
				return
			}
		case <-ctx.Done():
			logger.Warn("veritabani ping zaman asimi")
			WriteError(w, http.StatusServiceUnavailable, "unavailable", "database unreachable")
			return
		}

		WriteJSON(w, http.StatusOK, healthBody{
			Status: "ok",
			Checks: map[string]string{"database": "ok"},
		})
	}
}
