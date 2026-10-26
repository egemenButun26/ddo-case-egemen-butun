package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"parkakil/internal/domain/session"
	"parkakil/internal/service/sessionsvc"
)

const testKameraAnahtari = "test-kamera-anahtari-0123456789abcdef"

func newAuthRouter(key string, logger *slog.Logger, h EventHandler) http.Handler {
	return newRouter(Deps{Logger: logger, Pinger: fakePinger{}, PingTimeout: time.Second, Events: h, CameraKey: key}, nil)
}

func authRequest(t *testing.T, h http.Handler, body, contentType, key string, setKey bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/camera-events", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if setKey {
		req.Header.Set(cameraKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func yetkisizBekle(t *testing.T, rec *httptest.ResponseRecorder, fake *fakeEventHandler) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "unauthorized")
	if len(fake.got) != 0 {
		t.Errorf("servis cagri sayisi = %d, beklenen 0", len(fake.got))
	}
}

func TestCameraKeyGecerli(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{EventID: 42, Outcome: session.OutcomeOpened, SessionID: 7}, nil
	}}
	h := newAuthRouter(testKameraAnahtari, discardLogger(), fake)

	rec := authRequest(t, h, eventBody(t, nil), "application/json", testKameraAnahtari, true)

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201; govde: %s", rec.Code, rec.Body.String())
	}
	if len(fake.got) != 1 {
		t.Fatalf("servis cagri sayisi = %d, beklenen 1", len(fake.got))
	}
	body := decodeJSON(t, rec)
	if body["event_id"] != float64(42) {
		t.Errorf("event_id = %v, beklenen 42", body["event_id"])
	}
	if body["outcome"] != "opened" {
		t.Errorf("outcome = %v, beklenen opened", body["outcome"])
	}
	if body["session_id"] != float64(7) {
		t.Errorf("session_id = %v, beklenen 7", body["session_id"])
	}
}

func TestCameraKeyEksik(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newAuthRouter(testKameraAnahtari, discardLogger(), fake)

	rec := authRequest(t, h, eventBody(t, nil), "application/json", "", false)

	yetkisizBekle(t, rec, fake)
}

func TestCameraKeyBos(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newAuthRouter(testKameraAnahtari, discardLogger(), fake)

	rec := authRequest(t, h, eventBody(t, nil), "application/json", "", true)

	yetkisizBekle(t, rec, fake)
}

func TestCameraKeyGecersiz(t *testing.T) {
	cases := map[string]string{
		"yanlis ayni uzunluk": "baska-kamera-anahtari-0123456789ab",
		"onek":                testKameraAnahtari[:16],
		"fazla uzun":          testKameraAnahtari + "-fazla",
		"kisa":                testKameraAnahtari[:31],
	}
	for ad, anahtar := range cases {
		t.Run(ad, func(t *testing.T) {
			fake := &fakeEventHandler{}
			h := newAuthRouter(testKameraAnahtari, discardLogger(), fake)
			rec := authRequest(t, h, eventBody(t, nil), "application/json", anahtar, true)
			yetkisizBekle(t, rec, fake)
		})
	}
}

func TestCameraKeyYapilandirilmamis(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newAuthRouter("", discardLogger(), fake)

	rec := authRequest(t, h, eventBody(t, nil), "application/json", testKameraAnahtari, true)

	yetkisizBekle(t, rec, fake)
}

func TestCameraKeyIcerikKontrolundenOnce(t *testing.T) {
	cases := map[string]struct {
		body        string
		contentType string
	}{
		"anahtarsiz text/plain": {body: eventBody(t, nil), contentType: "text/plain"},
		"anahtarsiz bozuk json": {body: `{"lot_id":`, contentType: "application/json"},
	}
	for ad, c := range cases {
		t.Run(ad, func(t *testing.T) {
			fake := &fakeEventHandler{}
			h := newAuthRouter(testKameraAnahtari, discardLogger(), fake)
			rec := authRequest(t, h, c.body, c.contentType, "", false)
			yetkisizBekle(t, rec, fake)
		})
	}
}

func TestCameraKeyLoglanmaz(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fake := &fakeEventHandler{}
	h := newAuthRouter(testKameraAnahtari, logger, fake)

	gecerli := authRequest(t, h, eventBody(t, nil), "application/json", testKameraAnahtari, true)
	if gecerli.Code != http.StatusCreated {
		t.Fatalf("gecerli anahtar durum = %d, beklenen 201", gecerli.Code)
	}

	yanlisAnahtar := "yanlis-anahtar-0123456789abcdef"
	gecersiz := authRequest(t, h, eventBody(t, nil), "application/json", yanlisAnahtar, true)
	if gecersiz.Code != http.StatusUnauthorized {
		t.Fatalf("gecersiz anahtar durum = %d, beklenen 401", gecersiz.Code)
	}

	log := buf.String()
	if strings.Contains(log, testKameraAnahtari) {
		t.Errorf("gecerli anahtar loga yazilmamali: %s", log)
	}
	if strings.Contains(log, yanlisAnahtar) {
		t.Errorf("gecersiz anahtar loga yazilmamali: %s", log)
	}
	if !strings.Contains(log, "kamera anahtari reddedildi") {
		t.Errorf("ret logu beklenen metni icermeli: %s", log)
	}
}

func TestCameraKeyHealthAnahtarsiz(t *testing.T) {
	h := newAuthRouter(testKameraAnahtari, discardLogger(), &fakeEventHandler{})

	rec := doRequest(t, h, http.MethodGet, "/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
}
