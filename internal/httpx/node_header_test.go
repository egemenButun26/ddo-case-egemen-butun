package httpx

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const testNodeHeader = "X-DDO-Node"
const testNodeValue = "45a6ab"

func TestDdoNodeHeaderOnAllResponses(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)

	cases := []struct {
		ad   string
		path string
	}{
		{"saglik", "/health"},
		{"404", "/bilinmeyen"},
		{"401", "/auth/me"},
		{"hz", "/_hz/45a6"},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			rec := doRequest(t, h, http.MethodGet, c.path, nil)
			if got := rec.Header().Get(testNodeHeader); got != testNodeValue {
				t.Fatalf("%s basligi = %q, beklenen %q", testNodeHeader, got, testNodeValue)
			}
		})
	}

	rec := doRequest(t, h, http.MethodPost, "/health", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("405 durumu = %d", rec.Code)
	}
	if got := rec.Header().Get(testNodeHeader); got != testNodeValue {
		t.Fatalf("405 %s basligi = %q, beklenen %q", testNodeHeader, got, testNodeValue)
	}
}

func TestDdoNodeHeaderOnPanic(t *testing.T) {
	h := newRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second}, func(r chi.Router) {
		r.Get("/panic", func(http.ResponseWriter, *http.Request) {
			panic("test panik")
		})
	})
	rec := doRequest(t, h, http.MethodGet, "/panic", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	if got := rec.Header().Get(testNodeHeader); got != testNodeValue {
		t.Fatalf("%s basligi = %q, beklenen %q", testNodeHeader, got, testNodeValue)
	}
}

func TestHzEndpoint(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/_hz/45a6", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("status = %v, beklenen ok", body["status"])
	}
}
