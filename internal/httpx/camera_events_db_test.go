package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

var dbSayac atomic.Int64

func benzersiz(onek string) string {
	return fmt.Sprintf("%s-%d-%d", onek, time.Now().UnixNano(), dbSayac.Add(1))
}

func dbHazirla(t *testing.T) (http.Handler, *pgxpool.Pool, int64) {
	t.Helper()
	pool := testdb.Open(t)
	svc, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID := testdb.NewLot(t, pool, 100)
	v := tariff.Version{
		ID:        benzersiz("tarife"),
		ValidFrom: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:  time.UTC,
		Weekday: tariff.DayRule{
			Name:  "hafta_ici",
			Bands: []tariff.Band{{UnitMinutes: 60, UnitPrice: 500}},
		},
	}
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	return NewRouter(Deps{Logger: discardLogger(), Pinger: pool, PingTimeout: time.Second, Events: svc, CameraKey: testKameraAnahtari, DB: pool, SessionTTL: time.Hour}), pool, lotID
}

func postEventWithKey(t *testing.T, h http.Handler, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/camera-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(cameraKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postEvent(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postEventWithKey(t, h, body, testKameraAnahtari)
}

func eventJSON(lotID int64, cameraEventID, direction, plate, capturedAt string) string {
	b, err := json.Marshal(map[string]any{
		"lot_id":          lotID,
		"camera_id":       "cam-test",
		"camera_event_id": cameraEventID,
		"direction":       direction,
		"plate_raw":       plate,
		"captured_at":     capturedAt,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestCameraEventHTTPAkis(t *testing.T) {
	h, pool, lotID := dbHazirla(t)

	girisGovde := eventJSON(lotID, benzersiz("giris"), "entry", "34ABC34", "2026-03-01T10:00:00Z")
	rec := postEvent(t, h, girisGovde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("giris durum = %d, beklenen 201; govde: %s", rec.Code, rec.Body.String())
	}
	giris := decodeJSON(t, rec)
	if giris["outcome"] != "opened" {
		t.Fatalf("giris outcome = %v, beklenen opened", giris["outcome"])
	}
	if giris["session_id"] == nil {
		t.Fatalf("giris session_id null olmamali")
	}
	girisEventID := giris["event_id"]

	rec = postEvent(t, h, girisGovde)
	if rec.Code != http.StatusOK {
		t.Fatalf("tekrar durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	tekrar := decodeJSON(t, rec)
	if tekrar["replayed"] != true {
		t.Errorf("replayed = %v, beklenen true", tekrar["replayed"])
	}
	if tekrar["event_id"] != girisEventID {
		t.Errorf("tekrar event_id = %v, beklenen %v", tekrar["event_id"], girisEventID)
	}

	cikisCameraEventID := benzersiz("cikis")
	cikisGovde := eventJSON(lotID, cikisCameraEventID, "exit", "34ABC34", "2026-03-01T12:00:00Z")
	rec = postEvent(t, h, cikisGovde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cikis durum = %d, beklenen 201; govde: %s", rec.Code, rec.Body.String())
	}
	cikis := decodeJSON(t, rec)
	if cikis["outcome"] != "closed" {
		t.Fatalf("cikis outcome = %v, beklenen closed", cikis["outcome"])
	}
	tutar, ok := cikis["amount_kurus"].(float64)
	if !ok || tutar <= 0 {
		t.Errorf("cikis amount_kurus = %v, pozitif sayi olmali", cikis["amount_kurus"])
	}

	cakismaGovde := eventJSON(lotID, cikisCameraEventID, "exit", "34ZZZ34", "2026-03-01T12:00:00Z")
	rec = postEvent(t, h, cakismaGovde)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cakisma durum = %d, beklenen 409; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "event_conflict")

	olmayanLot := eventJSON(1<<60, benzersiz("yok"), "entry", "34ABC34", "2026-03-01T10:00:00Z")
	rec = postEvent(t, h, olmayanLot)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan lot durum = %d, beklenen 404; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "lot_not_found")

	gecersizYon := eventJSON(lotID, benzersiz("yon"), "side", "34ABC34", "2026-03-01T10:00:00Z")
	rec = postEvent(t, h, gecersizYon)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("gecersiz yon durum = %d, beklenen 422; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "invalid_event")

	var olaySayisi, oturumSayisi int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID).Scan(&olaySayisi); err != nil {
		t.Fatalf("olay sayisi okunamadi: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID).Scan(&oturumSayisi); err != nil {
		t.Fatalf("oturum sayisi okunamadi: %v", err)
	}
	if olaySayisi != 2 {
		t.Errorf("plate_events = %d, beklenen 2", olaySayisi)
	}
	if oturumSayisi != 1 {
		t.Errorf("sessions = %d, beklenen 1", oturumSayisi)
	}
}

func TestCameraEventHTTPEszamanli(t *testing.T) {
	h, pool, lotID := dbHazirla(t)

	const istek = 20
	sonuclar := make([]*httptest.ResponseRecorder, istek)
	var wg sync.WaitGroup
	bas := make(chan struct{})
	wg.Add(istek)
	for i := 0; i < istek; i++ {
		go func(i int) {
			defer wg.Done()
			<-bas
			govde := eventJSON(lotID, benzersiz("eszamanli"), "entry", "34ABC34", "2026-03-01T10:00:00Z")
			sonuclar[i] = postEvent(t, h, govde)
		}(i)
	}
	close(bas)
	wg.Wait()

	opened, duplicate := 0, 0
	for i, rec := range sonuclar {
		if rec.Code != http.StatusCreated {
			t.Fatalf("istek %d durum = %d, beklenen 201; govde: %s", i, rec.Code, rec.Body.String())
		}
		body := decodeJSON(t, rec)
		switch body["outcome"] {
		case "opened":
			opened++
		case "duplicate_entry":
			duplicate++
		default:
			t.Errorf("istek %d outcome = %v", i, body["outcome"])
		}
	}
	if opened != 1 {
		t.Errorf("opened = %d, beklenen 1", opened)
	}
	if duplicate != istek-1 {
		t.Errorf("duplicate_entry = %d, beklenen %d", duplicate, istek-1)
	}

	var acik int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID).Scan(&acik); err != nil {
		t.Fatalf("acik oturum sayisi okunamadi: %v", err)
	}
	if acik != 1 {
		t.Errorf("acik oturum = %d, beklenen 1", acik)
	}
}

func TestCameraEventHTTPYetkisizYazmaz(t *testing.T) {
	h, pool, lotID := dbHazirla(t)
	govde := eventJSON(lotID, benzersiz("yetkisiz"), "entry", "34ABC34", "2026-03-01T10:00:00Z")

	rec := postEventWithKey(t, h, govde, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anahtarsiz durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "unauthorized")

	rec = postEventWithKey(t, h, govde, "yanlis-kamera-anahtari")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yanlis anahtar durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "unauthorized")

	var olaySayisi, oturumSayisi int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID).Scan(&olaySayisi); err != nil {
		t.Fatalf("olay sayisi okunamadi: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID).Scan(&oturumSayisi); err != nil {
		t.Fatalf("oturum sayisi okunamadi: %v", err)
	}
	if olaySayisi != 0 {
		t.Errorf("plate_events = %d, beklenen 0", olaySayisi)
	}
	if oturumSayisi != 0 {
		t.Errorf("sessions = %d, beklenen 0", oturumSayisi)
	}
}
