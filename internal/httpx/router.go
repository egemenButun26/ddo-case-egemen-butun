package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"parkakil/internal/auth"
	"parkakil/internal/store"
)

type Deps struct {
	Logger      *slog.Logger
	Pinger      Pinger
	PingTimeout time.Duration
	Events      EventHandler
	CameraKey   string
	DB          store.Querier
	SessionTTL  time.Duration
}

func NewRouter(deps Deps) http.Handler {
	return newRouter(deps, nil)
}

func newRouter(deps Deps, extra func(chi.Router)) http.Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ttl := deps.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	deps.Logger = logger
	deps.SessionTTL = ttl

	r := chi.NewRouter()
	r.Use(requestID, accessLog(logger), recoverPanic(logger))

	r.Get("/health", healthHandler(deps.Pinger, deps.PingTimeout, logger))

	if deps.Events != nil {
		r.With(requireCameraKey(deps.CameraKey, logger)).Post("/camera-events", cameraEventHandler(deps.Events, logger))
	}

	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", loginHandler(deps))
		r.With(requireAuth(deps)).Post("/logout", logoutHandler(deps))
		r.With(requireAuth(deps)).Get("/me", meHandler())
	})

	r.With(requireAuth(deps), requireRole(auth.RoleAdmin)).Get("/admin/users", adminUsersHandler(deps))

	if extra != nil {
		extra(r)
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return r
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
			r.Header.Set("X-Request-ID", id)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("istek",
				"metot", r.Method,
				"yol", r.URL.Path,
				"durum", sw.status,
				"sure_ms", time.Since(start).Milliseconds(),
				"request_id", r.Header.Get("X-Request-ID"),
			)
		})
	}
}

func recoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panik", "hata", fmt.Sprint(rec), "request_id", r.Header.Get("X-Request-ID"))
					WriteError(w, http.StatusInternalServerError, "internal", "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
