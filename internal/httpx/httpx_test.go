package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fakePinger struct {
	fn func(ctx context.Context) error
}

func (f fakePinger) Ping(ctx context.Context) error {
	if f.fn == nil {
		return nil
	}
	return f.fn(ctx)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestRouter(t *testing.T, p Pinger, timeout time.Duration) http.Handler {
	t.Helper()
	return NewRouter(discardLogger(), p, timeout)
}

func doRequest(t *testing.T, h http.Handler, method, path string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, values := range header {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("govde JSON degil (%q): %v", rec.Body.String(), err)
	}
	return body
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := decodeJSON(t, rec)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("hata govdesi eksik: %v", body)
	}
	if got := errObj["code"]; got != code {
		t.Errorf("code = %v, beklenen %q", got, code)
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Errorf("message eksik ya da metin degil: %v", errObj)
	}
}

func TestHealthOK(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok || checks["database"] != "ok" {
		t.Errorf("checks = %v", body["checks"])
	}
}

func TestHealthPingError(t *testing.T) {
	h := newTestRouter(t, fakePinger{fn: func(context.Context) error {
		return errors.New("ham hata: gizli ayrinti")
	}}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/health", nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("durum = %d, beklenen 503", rec.Code)
	}
	assertErrorBody(t, rec, "unavailable")
	if strings.Contains(rec.Body.String(), "gizli ayrinti") {
		t.Errorf("ham hata govdede olmamali: %q", rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	errObj := body["error"].(map[string]any)
	if errObj["message"] != "database unreachable" {
		t.Errorf("message = %v", errObj["message"])
	}
}

func TestHealthPingTimeout(t *testing.T) {
	h := newTestRouter(t, fakePinger{fn: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}, 50*time.Millisecond)

	start := time.Now()
	rec := doRequest(t, h, http.MethodGet, "/health", nil)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("durum = %d, beklenen 503", rec.Code)
	}
	if elapsed >= time.Second {
		t.Errorf("istek %v surdu, 1 sn'den kisa olmali", elapsed)
	}
	assertErrorBody(t, rec, "unavailable")
}

func TestNotFound(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/bilinmeyen", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("durum = %d, beklenen 404", rec.Code)
	}
	assertErrorBody(t, rec, "not_found")
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodPost, "/health", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("durum = %d, beklenen 405", rec.Code)
	}
	assertErrorBody(t, rec, "method_not_allowed")
}

func TestPanicRecovered(t *testing.T) {
	h := newRouter(discardLogger(), fakePinger{}, time.Second, func(r chi.Router) {
		r.Get("/panic", func(http.ResponseWriter, *http.Request) {
			panic("beklenmeyen durum")
		})
	})
	rec := doRequest(t, h, http.MethodGet, "/panic", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	assertErrorBody(t, rec, "internal")
}

func TestRequestIDPassthrough(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	header := http.Header{"X-Request-Id": []string{"gelen-id-123"}}
	rec := doRequest(t, h, http.MethodGet, "/health", header)

	if got := rec.Header().Get("X-Request-ID"); got != "gelen-id-123" {
		t.Errorf("X-Request-ID = %q, beklenen gelen deger", got)
	}
}

func TestRequestIDGenerated(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/health", nil)

	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID bos olmamali")
	}
}

func TestHealthHandlerExported(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	HealthHandler(fakePinger{}, time.Second).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}
}
