package httpx

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenAPIDocumentServed(t *testing.T) {
	h := newTestRouter(t, fakePinger{}, time.Second)
	rec := doRequest(t, h, http.MethodGet, "/openapi.yaml", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if body != string(openapiSpec) {
		t.Errorf("gövde gömülü dokümanla aynı değil (uzunluk %d != %d)", len(body), len(openapiSpec))
	}
	if !strings.HasPrefix(body, "openapi: 3.1.0") {
		t.Errorf("doküman OpenAPI 3.1.0 ile başlamıyor: %q", body[:min(40, len(body))])
	}
}
