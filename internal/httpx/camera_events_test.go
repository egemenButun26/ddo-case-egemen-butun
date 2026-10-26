package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/sessionsvc"
)

type fakeEventHandler struct {
	fn  func(ctx context.Context, ev session.Event) (sessionsvc.Result, error)
	got []session.Event
}

func (f *fakeEventHandler) HandleEvent(ctx context.Context, ev session.Event) (sessionsvc.Result, error) {
	f.got = append(f.got, ev)
	if f.fn == nil {
		return sessionsvc.Result{EventID: 1, Outcome: session.OutcomeOpened}, nil
	}
	return f.fn(ctx, ev)
}

func newEventRouter(h EventHandler) http.Handler {
	return newRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second, Events: h, CameraKey: testKameraAnahtari}, nil)
}

func eventBody(t *testing.T, alanlar map[string]any) string {
	t.Helper()
	temel := map[string]any{
		"lot_id":          int64(1),
		"camera_id":       "cam-giris",
		"camera_event_id": "olay-1",
		"direction":       "entry",
		"plate_raw":       "34ABC34",
		"captured_at":     "2026-01-01T10:00:00+03:00",
	}
	for k, v := range alanlar {
		temel[k] = v
	}
	b, err := json.Marshal(temel)
	if err != nil {
		t.Fatalf("govde olusturulamadi: %v", err)
	}
	return string(b)
}

func doEventRequest(t *testing.T, h http.Handler, method, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/camera-events", strings.NewReader(body))
	req.Header.Set(cameraKeyHeader, testKameraAnahtari)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCameraEventOpened201(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{EventID: 42, Outcome: session.OutcomeOpened, SessionID: 7}, nil
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["event_id"] != float64(42) {
		t.Errorf("event_id = %v", body["event_id"])
	}
	if body["outcome"] != "opened" {
		t.Errorf("outcome = %v", body["outcome"])
	}
	if body["session_id"] != float64(7) {
		t.Errorf("session_id = %v", body["session_id"])
	}
	if body["replayed"] != false {
		t.Errorf("replayed = %v", body["replayed"])
	}
	if body["closed_by_orphan_exit"] != false {
		t.Errorf("closed_by_orphan_exit = %v", body["closed_by_orphan_exit"])
	}
}

func TestCameraEventReplayed200(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{EventID: 42, Outcome: session.OutcomeOpened, SessionID: 7, Replayed: true}, nil
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["replayed"] != true {
		t.Errorf("replayed = %v", body["replayed"])
	}
	if body["event_id"] != float64(42) {
		t.Errorf("event_id = %v", body["event_id"])
	}
}

func TestCameraEventNullFields(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{EventID: 5, Outcome: session.OutcomeOrphanExit}, nil
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201", rec.Code)
	}
	body := decodeJSON(t, rec)
	for _, alan := range []string{"session_id", "review_id", "amount_kurus"} {
		if _, ok := body[alan]; !ok {
			t.Errorf("%q alani govdede yok: %v", alan, body)
		}
		if body[alan] != nil {
			t.Errorf("%q = %v, beklenen null", alan, body[alan])
		}
	}
}

func TestCameraEventAmountKurusClosed(t *testing.T) {
	tutar := tariff.Kurus(15000)
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{
			EventID:     9,
			Outcome:     session.OutcomeClosed,
			SessionID:   3,
			AmountKurus: &tutar,
		}, nil
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, map[string]any{"direction": "exit"}), "application/json")

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["outcome"] != "closed" {
		t.Errorf("outcome = %v", body["outcome"])
	}
	if body["amount_kurus"] != float64(15000) {
		t.Errorf("amount_kurus = %v, beklenen 15000", body["amount_kurus"])
	}
}

func TestCameraEventUnsupportedMediaType(t *testing.T) {
	h := newEventRouter(&fakeEventHandler{})

	for ad, ct := range map[string]string{
		"content-type yok": "",
		"text/plain":       "text/plain",
	} {
		t.Run(ad, func(t *testing.T) {
			rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), ct)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("durum = %d, beklenen 415", rec.Code)
			}
			assertErrorBody(t, rec, "unsupported_media_type")
		})
	}
}

func TestCameraEventContentTypeParametreli(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json; charset=utf-8")

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201", rec.Code)
	}
	if len(fake.got) != 1 {
		t.Fatalf("handler cagri sayisi = %d, beklenen 1", len(fake.got))
	}
}

func TestCameraEventPayloadTooLarge(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newEventRouter(fake)
	body := eventBody(t, map[string]any{"plate_raw": strings.Repeat("A", 70*1024)})
	rec := doEventRequest(t, h, http.MethodPost, body, "application/json")

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("durum = %d, beklenen 413", rec.Code)
	}
	assertErrorBody(t, rec, "payload_too_large")
	if len(fake.got) != 0 {
		t.Errorf("handler cagrilmamaliydi")
	}
}

func TestCameraEventInvalidJSON(t *testing.T) {
	h := newEventRouter(&fakeEventHandler{})

	cases := map[string]string{
		"bozuk JSON":        `{"lot_id":`,
		"bilinmeyen alan":   eventBody(t, map[string]any{"fazla": 1}),
		"fazladan veri":     eventBody(t, nil) + `{"lot_id":1}`,
		"bos govde":         ``,
		"yanlis tip lot_id": eventBody(t, map[string]any{"lot_id": "x"}),
		"saat dilimsiz an":  eventBody(t, map[string]any{"captured_at": "2026-01-01T10:00:00"}),
		"gecersiz zaman":    eventBody(t, map[string]any{"captured_at": "dun"}),
	}

	for ad, body := range cases {
		t.Run(ad, func(t *testing.T) {
			rec := doEventRequest(t, h, http.MethodPost, body, "application/json")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("durum = %d, beklenen 400", rec.Code)
			}
			assertErrorBody(t, rec, "invalid_json")
		})
	}
}

func TestCameraEventInvalidEvent422(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{}, fmt.Errorf("%w: direction", session.ErrInvalidEvent)
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("durum = %d, beklenen 422", rec.Code)
	}
	assertErrorBody(t, rec, "invalid_event")
	body := decodeJSON(t, rec)
	errObj := body["error"].(map[string]any)
	if !strings.Contains(errObj["message"].(string), "direction") {
		t.Errorf("message = %v, alan adini icermeli", errObj["message"])
	}
}

func TestCameraEventLotNotFound404(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{}, fmt.Errorf("sarmal: %w", sessionsvc.ErrLotNotFound)
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("durum = %d, beklenen 404", rec.Code)
	}
	assertErrorBody(t, rec, "lot_not_found")
}

func TestCameraEventConflict409(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{}, fmt.Errorf("olay cakismasi: %w", sessionsvc.ErrEventConflict)
	}}
	if !errors.Is(fmt.Errorf("sarmal: %w", sessionsvc.ErrEventConflict), sessionsvc.ErrEventConflict) {
		t.Fatal("errors.Is sarmalanmis hatayi bulamadi")
	}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusConflict {
		t.Fatalf("durum = %d, beklenen 409", rec.Code)
	}
	assertErrorBody(t, rec, "event_conflict")
}

func TestCameraEventInternal500(t *testing.T) {
	fake := &fakeEventHandler{fn: func(context.Context, session.Event) (sessionsvc.Result, error) {
		return sessionsvc.Result{}, errors.New("gizli ic ayrinti")
	}}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	assertErrorBody(t, rec, "internal")
	if strings.Contains(rec.Body.String(), "gizli ic ayrinti") {
		t.Errorf("ic hata metni govdede olmamali: %q", rec.Body.String())
	}
}

func TestCameraEventMethodNotAllowed(t *testing.T) {
	h := newEventRouter(&fakeEventHandler{})
	rec := doEventRequest(t, h, http.MethodGet, "", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("durum = %d, beklenen 405", rec.Code)
	}
	assertErrorBody(t, rec, "method_not_allowed")
}

func TestCameraEventRequestID(t *testing.T) {
	h := newEventRouter(&fakeEventHandler{})
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, nil), "application/json")

	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID bos olmamali")
	}
}

func TestCameraEventAlanEslestirme(t *testing.T) {
	fake := &fakeEventHandler{}
	h := newEventRouter(fake)
	rec := doEventRequest(t, h, http.MethodPost, eventBody(t, map[string]any{
		"lot_id":          int64(77),
		"camera_id":       "cam-cikis",
		"camera_event_id": "olay-77",
		"direction":       "exit",
		"plate_raw":       "06XYZ06",
		"captured_at":     "2026-02-03T04:05:06+03:00",
	}), "application/json")

	if rec.Code != http.StatusCreated {
		t.Fatalf("durum = %d, beklenen 201", rec.Code)
	}
	if len(fake.got) != 1 {
		t.Fatalf("handler cagri sayisi = %d, beklenen 1", len(fake.got))
	}
	ev := fake.got[0]
	if ev.LotID != 77 || ev.CameraID != "cam-cikis" || ev.CameraEventID != "olay-77" {
		t.Errorf("kimlik alanlari yanlis: %+v", ev)
	}
	if ev.Direction != session.Exit {
		t.Errorf("direction = %q, beklenen exit", ev.Direction)
	}
	if ev.PlateRaw != "06XYZ06" {
		t.Errorf("plate_raw = %q", ev.PlateRaw)
	}
	_, offset := ev.CapturedAt.Zone()
	if offset != 3*60*60 {
		t.Errorf("saat dilimi korunmadi, offset = %d", offset)
	}
	if !ev.CapturedAt.Equal(time.Date(2026, 2, 3, 4, 5, 6, 0, time.FixedZone("", 3*60*60))) {
		t.Errorf("captured_at = %v", ev.CapturedAt)
	}
}
