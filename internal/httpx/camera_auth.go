package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
)

const cameraKeyHeader = "X-Camera-Key"

func requireCameraKey(cameraKey string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get(cameraKeyHeader)
			if !validCameraKey(cameraKey, provided) {
				neden := "gecersiz"
				if provided == "" {
					neden = "eksik"
				}
				logger.Warn("kamera anahtari reddedildi",
					"request_id", r.Header.Get("X-Request-ID"),
					"neden", neden,
				)
				WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid camera key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validCameraKey(cameraKey, provided string) bool {
	if cameraKey == "" || provided == "" {
		return false
	}
	expected := sha256.Sum256([]byte(cameraKey))
	actual := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}
