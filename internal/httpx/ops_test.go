package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"parkakil/internal/auth"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

type opsEnv struct {
	h         http.Handler
	pool      *pgxpool.Pool
	sessions  *sessionsvc.Service
	occupancy *occupancysvc.Service
	lotID     int64
	tariffID  string
}

func opsHazirla(t *testing.T, capacity int) *opsEnv {
	t.Helper()
	pool := testdb.Open(t)
	sessionSvc, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		t.Fatalf("sessionsvc olusturulamadi: %v", err)
	}
	occupancySvc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("occupancysvc olusturulamadi: %v", err)
	}
	lotID, tariffID := opsLotEkle(t, pool, capacity)
	h := NewRouter(Deps{
		Logger:      discardLogger(),
		Pinger:      pool,
		PingTimeout: time.Second,
		Events:      sessionSvc,
		CameraKey:   testKameraAnahtari,
		DB:          pool,
		SessionTTL:  time.Hour,
		Occupancy:   occupancySvc,
	})
	return &opsEnv{h: h, pool: pool, sessions: sessionSvc, occupancy: occupancySvc, lotID: lotID, tariffID: tariffID}
}

func opsLotEkle(t *testing.T, pool *pgxpool.Pool, capacity int) (int64, string) {
	t.Helper()
	lotID := testdb.NewLot(t, pool, capacity)
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
	return lotID, v.ID
}

func opsKullanici(t *testing.T, pool *pgxpool.Pool, role auth.Role, plateRaw string) store.User {
	t.Helper()
	hash, err := auth.HashPasswordCost(authTestParola, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("parola ozeti: %v", err)
	}
	var platePtr *string
	if plateRaw != "" {
		p := plate.Normalize(plateRaw)
		platePtr = &p
	}
	email := strings.ToLower(benzersiz("ops")) + "@test.local"
	id, err := store.CreateUser(context.Background(), pool, store.CreateUserParams{
		Email:        email,
		Name:         "Ops Kullanici",
		Role:         role,
		PasswordHash: hash,
		Plate:        platePtr,
	})
	if err != nil {
		t.Fatalf("kullanici olusturulamadi: %v", err)
	}
	user, err := store.GetUser(context.Background(), pool, id)
	if err != nil {
		t.Fatalf("kullanici okunamadi: %v", err)
	}
	return user
}

func opsToken(t *testing.T, h http.Handler, pool *pgxpool.Pool, role auth.Role, plateRaw string) (store.User, string) {
	t.Helper()
	user := opsKullanici(t, pool, role, plateRaw)
	giris := authLogin(t, h, user.Email, authTestParola)
	return user, giris.Token
}

func opsDo(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("govde hazirlanamadi: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func opsListe(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("liste govdesi cozulemedi (%q): %v", rec.Body.String(), err)
	}
	return out
}

func opsOlay(t *testing.T, pool *pgxpool.Pool, lotID int64, yon session.Direction, plateRaw string, at time.Time, outcome session.Outcome) int64 {
	t.Helper()
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-ops",
		CameraEventID: benzersiz("ops-olay"),
		Direction:     yon,
		PlateRaw:      plateRaw,
		CapturedAt:    at,
	}
	id, err := store.InsertEvent(context.Background(), pool, ev, plate.Normalize(plateRaw), outcome, 0)
	if err != nil {
		t.Fatalf("olay eklenemedi: %v", err)
	}
	return id
}

func TestOpsLotsYetki(t *testing.T) {
	env := opsHazirla(t, 10)

	rec := opsDo(t, env.h, http.MethodGet, "/lots", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "unauthorized")

	for _, role := range []auth.Role{auth.RoleDriver, auth.RoleAttendant, auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			_, token := opsToken(t, env.h, env.pool, role, "")
			rec := opsDo(t, env.h, http.MethodGet, "/lots", token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
			}
			lots := opsListe(t, rec)
			bulundu := false
			for _, lot := range lots {
				if int64(lot["id"].(float64)) == env.lotID {
					bulundu = true
					if int(lot["capacity"].(float64)) != 10 {
						t.Fatalf("kapasite = %v, beklenen 10", lot["capacity"])
					}
					if lot["code"] == "" || lot["name"] == "" {
						t.Fatalf("lot alanlari eksik: %v", lot)
					}
				}
			}
			if !bulundu {
				t.Fatalf("lot listede yok")
			}
		})
	}
}

func TestOpsOccupancy(t *testing.T) {
	env := opsHazirla(t, 3)
	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "34OCC001")
	_, gorevli := opsToken(t, env.h, env.pool, auth.RoleAttendant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	rec := opsDo(t, env.h, http.MethodGet, "/occupancy", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")

	rec = opsDo(t, env.h, http.MethodGet, "/occupancy", sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d, beklenen 403", rec.Code)
	}
	assertErrorBody(t, rec, "forbidden")

	if rec := opsDo(t, env.h, http.MethodPost, "/reservations", sofor,
		map[string]any{"lot_id": env.lotID, "plate": "34OCC001"}); rec.Code != http.StatusCreated {
		t.Fatalf("rezervasyon durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	if rec := postEvent(t, env.h, eventJSON(env.lotID, benzersiz("occ-giris"), "entry", "34OCC002", "2026-03-01T10:00:00Z")); rec.Code != http.StatusCreated {
		t.Fatalf("giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}

	for ad, token := range map[string]string{"gorevli": gorevli, "mudur": mudur, "yonetici": yonetici} {
		t.Run(ad, func(t *testing.T) {
			rec := opsDo(t, env.h, http.MethodGet, "/occupancy", token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
			}
			rows := opsListe(t, rec)
			var bizim map[string]any
			for _, row := range rows {
				if int64(row["lot_id"].(float64)) == env.lotID {
					bizim = row
				}
			}
			if bizim == nil {
				t.Fatalf("lot doluluk satiri yok")
			}
			if int(bizim["capacity"].(float64)) != 3 || int(bizim["occupied"].(float64)) != 1 ||
				int(bizim["active_reservations"].(float64)) != 1 || int(bizim["available"].(float64)) != 1 {
				t.Fatalf("doluluk alanlari yanlis: %v", bizim)
			}
		})
	}
}

func TestOpsReservations(t *testing.T) {
	env := opsHazirla(t, 5)
	densizLot, _ := opsLotEkle(t, env.pool, 1)

	_, sofor1 := opsToken(t, env.h, env.pool, auth.RoleDriver, "34OPS123")
	_, sofor2 := opsToken(t, env.h, env.pool, auth.RoleDriver, "34OPS456")
	_, muhasebeci := opsToken(t, env.h, env.pool, auth.RoleAccountant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")

	rec := opsDo(t, env.h, http.MethodPost, "/reservations", "", map[string]any{"lot_id": env.lotID, "plate": "34OPS123"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, "/reservations", muhasebeci, map[string]any{"lot_id": env.lotID, "plate": "34OPS123"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("muhasebeci durum = %d, beklenen 403", rec.Code)
	}

	rec = opsDo(t, env.h, http.MethodPost, "/reservations", sofor1, map[string]any{"lot_id": env.lotID, "plate": "34 ops 123"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("kendi plaka durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["plate"] != "34OPS123" || int64(body["lot_id"].(float64)) != env.lotID {
		t.Fatalf("rezervasyon alanlari yanlis: %v", body)
	}
	ilkID := int64(body["id"].(float64))

	rec = opsDo(t, env.h, http.MethodPost, "/reservations", sofor1, map[string]any{"lot_id": env.lotID, "plate": "34OPS456"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baska plaka durum = %d, beklenen 403", rec.Code)
	}
	assertErrorBody(t, rec, "forbidden")

	rec = opsDo(t, env.h, http.MethodPost, "/reservations", sofor1, map[string]any{"lot_id": env.lotID, "plate": "34OPS123"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("tekrar rezervasyon durum = %d, beklenen 409", rec.Code)
	}
	assertErrorBody(t, rec, "reservation_exists")

	rec = opsDo(t, env.h, http.MethodPost, "/reservations", mudur, map[string]any{"lot_id": env.lotID, "plate": "34OPS789"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("mudur rezervasyon durum = %d; govde: %s", rec.Code, rec.Body.String())
	}

	if rec := opsDo(t, env.h, http.MethodPost, "/reservations", mudur,
		map[string]any{"lot_id": densizLot, "plate": "34CAP001"}); rec.Code != http.StatusCreated {
		t.Fatalf("dolu lot ilk rezervasyon durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	rec = opsDo(t, env.h, http.MethodPost, "/reservations", mudur, map[string]any{"lot_id": densizLot, "plate": "34CAP002"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("kapasite dolu durum = %d, beklenen 409", rec.Code)
	}
	assertErrorBody(t, rec, "no_capacity")

	rec = opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/reservations/%d", ilkID), sofor2, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baska sofor silme durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/reservations/%d", ilkID), mudur, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mudur silme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}

	rec = opsDo(t, env.h, http.MethodPost, "/reservations", sofor1, map[string]any{"lot_id": env.lotID, "plate": "34OPS123"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("yeniden rezervasyon durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	yeniID := int64(decodeJSON(t, rec)["id"].(float64))
	rec = opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/reservations/%d", yeniID), sofor1, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("kendi rezervasyon silme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}

	rec = opsDo(t, env.h, http.MethodDelete, "/reservations/999999999", mudur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan rezervasyon durum = %d, beklenen 404", rec.Code)
	}
}

func TestOpsBlacklistUclari(t *testing.T) {
	env := opsHazirla(t, 10)
	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "")
	_, gorevli := opsToken(t, env.h, env.pool, auth.RoleAttendant, "")
	_, muhasebeci := opsToken(t, env.h, env.pool, auth.RoleAccountant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	rec := opsDo(t, env.h, http.MethodGet, "/blacklist", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")

	rec = opsDo(t, env.h, http.MethodGet, "/blacklist", sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor listeleme durum = %d, beklenen 403", rec.Code)
	}
	assertErrorBody(t, rec, "forbidden")

	for ad, token := range map[string]string{"gorevli": gorevli, "mudur": mudur, "yonetici": yonetici} {
		t.Run("listele-"+ad, func(t *testing.T) {
			if rec := opsDo(t, env.h, http.MethodGet, "/blacklist", token, nil); rec.Code != http.StatusOK {
				t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
			}
		})
	}

	for ad, token := range map[string]string{"gorevli": gorevli, "muhasebeci": muhasebeci} {
		t.Run("ekle-yasak-"+ad, func(t *testing.T) {
			rec := opsDo(t, env.h, http.MethodPost, "/blacklist", token, map[string]any{"plate": "34BLK001", "reason": "test"})
			if rec.Code != http.StatusForbidden {
				t.Fatalf("durum = %d, beklenen 403", rec.Code)
			}
		})
	}

	rec = opsDo(t, env.h, http.MethodPost, "/blacklist", mudur, map[string]any{"plate": "34 blk 001", "reason": "  giris yasak  "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("ekleme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := decodeJSON(t, rec)
	if ilk["plate"] != "34BLK001" || ilk["reason"] != "giris yasak" || ilk["active"] != true {
		t.Fatalf("kara liste alanlari yanlis: %v", ilk)
	}
	ilkID := int64(ilk["id"].(float64))
	if ilk["created_by"] == nil {
		t.Fatalf("created_by bos")
	}

	rec = opsDo(t, env.h, http.MethodPost, "/blacklist", mudur, map[string]any{"plate": "34BLK001", "reason": "tekrar"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("tekrar ekleme durum = %d, beklenen 409", rec.Code)
	}
	assertErrorBody(t, rec, "blacklist_exists")

	rec = opsDo(t, env.h, http.MethodPost, "/blacklist", yonetici, map[string]any{"plate": "34BLK002", "reason": "test"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("ikinci ekleme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}

	rec = opsDo(t, env.h, http.MethodPost, "/blacklist", mudur, map[string]any{"plate": "34BLK003", "reason": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bos neden durum = %d, beklenen 400", rec.Code)
	}

	rec = opsDo(t, env.h, http.MethodGet, fmt.Sprintf("/blacklist?after_id=%d&limit=1", ilkID), gorevli, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("keyset durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	sayfa := decodeJSON(t, rec)
	items, _ := sayfa["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("keyset items = %d, beklenen 1", len(items))
	}
	oge := items[0].(map[string]any)
	if int64(oge["id"].(float64)) <= ilkID {
		t.Fatalf("keyset ihlali: %v <= %d", oge["id"], ilkID)
	}

	before := auditSayisi(t, env.pool, "blacklist.create")
	rec = opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/blacklist/%d", ilkID), gorevli, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("gorevli silme durum = %d, beklenen 403", rec.Code)
	}
	if rec := opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/blacklist/%d", ilkID), mudur, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("silme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	if after := auditSayisi(t, env.pool, "blacklist.remove"); after < 1 {
		t.Fatalf("blacklist.remove denetim kaydi yok")
	}
	if before < 1 {
		t.Fatalf("blacklist.create denetim kaydi yok")
	}

	blacklisted, err := store.IsPlateBlacklisted(context.Background(), env.pool, "34BLK001")
	if err != nil || blacklisted {
		t.Fatalf("silinen plaka kara listede: %v, hata %v", blacklisted, err)
	}
	rec = opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/blacklist/%d", ilkID), mudur, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ikinci silme durum = %d, beklenen 404", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodDelete, "/blacklist/abc", mudur, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("gecersiz id durum = %d, beklenen 400", rec.Code)
	}
}

func TestOpsBlacklistGiris(t *testing.T) {
	env := opsHazirla(t, 10)
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")

	rec := opsDo(t, env.h, http.MethodPost, "/blacklist", mudur, map[string]any{"plate": "34ABC34", "reason": "giris yasak"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("kara liste ekleme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	blacklistID := int64(decodeJSON(t, rec)["id"].(float64))

	rec = postEvent(t, env.h, eventJSON(env.lotID, benzersiz("blk-giris"), "entry", "34ABC34", "2026-03-01T10:00:00Z"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("kara liste giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["outcome"] != "review" || body["session_id"] != nil || body["review_id"] == nil {
		t.Fatalf("kara liste giris sonucu yanlis: %v", body)
	}
	var oturum int
	if err := env.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, env.lotID).Scan(&oturum); err != nil {
		t.Fatalf("oturum sayisi okunamadi: %v", err)
	}
	if oturum != 0 {
		t.Fatalf("kara liste girisinde oturum acildi: %d", oturum)
	}
	var reason string
	if err := env.pool.QueryRow(context.Background(),
		`SELECT reason FROM "ya-review_items" WHERE id = $1`, int64(body["review_id"].(float64))).Scan(&reason); err != nil {
		t.Fatalf("inceleme nedeni okunamadi: %v", err)
	}
	if reason != session.ReasonBlacklisted {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", reason, session.ReasonBlacklisted)
	}

	if rec := opsDo(t, env.h, http.MethodDelete, fmt.Sprintf("/blacklist/%d", blacklistID), mudur, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("kara liste silme durum = %d", rec.Code)
	}
	rec = postEvent(t, env.h, eventJSON(env.lotID, benzersiz("blk-giris2"), "entry", "34ABC34", "2026-03-01T10:05:00Z"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("listeden cikis sonrasi giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body = decodeJSON(t, rec)
	if body["outcome"] != "opened" || body["session_id"] == nil {
		t.Fatalf("listeden cikis sonrasi giris sonucu yanlis: %v", body)
	}
}

func TestOpsRecountVeDrifts(t *testing.T) {
	env := opsHazirla(t, 10)
	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	if rec := postEvent(t, env.h, eventJSON(env.lotID, benzersiz("drift-giris"), "entry", "34DRF001", "2026-03-01T10:00:00Z")); rec.Code != http.StatusCreated {
		t.Fatalf("giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	if err := store.SetCounter(context.Background(), env.pool, env.lotID, 6); err != nil {
		t.Fatalf("sayac bozulamadi: %v", err)
	}

	path := fmt.Sprintf("/admin/occupancy/%d/recount", env.lotID)
	rec := opsDo(t, env.h, http.MethodPost, path, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, path, sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, path, mudur, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mudur durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, path, yonetici, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("yonetici durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if int(body["stored"].(float64)) != 6 || int(body["recomputed"].(float64)) != 1 ||
		body["drifted"] != true || body["corrected"] != true {
		t.Fatalf("recount yaniti yanlis: %v", body)
	}
	if n, err := store.GetCounter(context.Background(), env.pool, env.lotID); err != nil || n != 1 {
		t.Fatalf("duzeltilmis sayac = %d, hata %v; beklenen 1", n, err)
	}

	if err := store.SetCounter(context.Background(), env.pool, env.lotID, 4); err != nil {
		t.Fatalf("sayac ikinci kez bozulamadi: %v", err)
	}
	if rec := opsDo(t, env.h, http.MethodPost, path, yonetici, nil); rec.Code != http.StatusOK {
		t.Fatalf("ikinci recount durum = %d", rec.Code)
	}

	rec = opsDo(t, env.h, http.MethodGet, "/admin/counter-drifts", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("kayma yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, "/admin/counter-drifts", sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("kayma sofor durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, "/admin/counter-drifts?limit=1", mudur, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("kayma mudur durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := decodeJSON(t, rec)
	items, _ := ilk["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("ilk sayfa items = %d, beklenen 1", len(items))
	}
	oge := items[0].(map[string]any)
	driftID := int64(oge["id"].(float64))
	if int64(oge["lot_id"].(float64)) != env.lotID {
		t.Fatalf("kayma lot = %v, beklenen %d", oge["lot_id"], env.lotID)
	}
	next := int64(ilk["next_after_id"].(float64))
	if next != driftID {
		t.Fatalf("next_after_id = %d, son oge = %d", next, driftID)
	}
	rec = opsDo(t, env.h, http.MethodGet, fmt.Sprintf("/admin/counter-drifts?after_id=%d", next), yonetici, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ikinci sayfa durum = %d", rec.Code)
	}
	ikinci := decodeJSON(t, rec)
	items2, _ := ikinci["items"].([]any)
	if len(items2) == 0 {
		t.Fatalf("ikinci sayfa bos")
	}
	for _, raw := range items2 {
		if int64(raw.(map[string]any)["id"].(float64)) <= next {
			t.Fatalf("keyset ihlali: %v <= %d", raw.(map[string]any)["id"], next)
		}
	}

	rec = opsDo(t, env.h, http.MethodPost, "/admin/occupancy/999999999/recount", yonetici, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan lot recount durum = %d, beklenen 404", rec.Code)
	}
}

func TestOpsReportsDaily(t *testing.T) {
	env := opsHazirla(t, 10)
	ikinciLot, _ := opsLotEkle(t, env.pool, 10)
	ctx := context.Background()

	gunBas := time.Date(2026, 2, 28, 21, 0, 0, 0, time.UTC)
	giris := opsOlay(t, env.pool, env.lotID, session.Entry, "34RPT001", gunBas.Add(11*time.Hour), session.OutcomeOpened)
	cikis := opsOlay(t, env.pool, env.lotID, session.Exit, "34RPT001", gunBas.Add(13*time.Hour), session.OutcomeClosed)
	sessID, err := store.InsertSession(ctx, env.pool, env.lotID, "34RPT001", giris, gunBas.Add(11*time.Hour), env.tariffID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}
	if err := store.SetEventOutcome(ctx, env.pool, cikis, session.OutcomeClosed, sessID); err != nil {
		t.Fatalf("cikis olayi baglanamadi: %v", err)
	}
	if err := store.CloseSession(ctx, env.pool, sessID, cikis, gunBas.Add(13*time.Hour), 1500, nil); err != nil {
		t.Fatalf("oturum kapatilamadi: %v", err)
	}

	if _, err := env.pool.Exec(ctx,
		`INSERT INTO "ya-payments" (session_id, amount_kurus, status, provider_ref, idempotency_key,
		   request_hash, requested_at, paid_at, confirmed_at)
		 VALUES ($1, 1000, 'succeeded', $2, $3, decode(repeat('ab', 32), 'hex'), $4, $5, $6)`,
		sessID, benzersiz("prov"), benzersiz("idem"),
		gunBas.Add(11*time.Hour+30*time.Minute), gunBas.Add(12*time.Hour+30*time.Minute), gunBas.Add(13*time.Hour),
	); err != nil {
		t.Fatalf("odeme eklenemedi: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO "ya-debts" (session_id, lot_id, plate, amount_kurus, paid_kurus, status)
		 VALUES ($1, $2, '34RPT001', 2000, 500, 'open')`,
		sessID, env.lotID,
	); err != nil {
		t.Fatalf("borc eklenemedi: %v", err)
	}

	incelemeOlay := opsOlay(t, env.pool, env.lotID, session.Entry, "34RPT002", gunBas.Add(35*time.Hour), session.OutcomeReview)
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO "ya-review_items" (event_id, lot_id, reason, read_plate, created_at)
		 VALUES ($1, $2, 'ambiguous', '34RPT002', $3)`,
		incelemeOlay, env.lotID, gunBas.Add(12*time.Hour),
	); err != nil {
		t.Fatalf("acik inceleme eklenemedi: %v", err)
	}
	kapaliOlay := opsOlay(t, env.pool, env.lotID, session.Entry, "34RPT003", gunBas.Add(36*time.Hour), session.OutcomeReview)
	if _, err := env.pool.Exec(ctx,
		`INSERT INTO "ya-review_items" (event_id, lot_id, reason, read_plate, status, created_at, resolved_at)
		 VALUES ($1, $2, 'ambiguous', '34RPT003', 'resolved', $3, $4)`,
		kapaliOlay, env.lotID, gunBas.Add(12*time.Hour), gunBas.Add(13*time.Hour),
	); err != nil {
		t.Fatalf("kapali inceleme eklenemedi: %v", err)
	}

	opsOlay(t, env.pool, ikinciLot, session.Entry, "34RPT004", gunBas.Add(12*time.Hour), session.OutcomeOpened)

	disariGiris := opsOlay(t, env.pool, env.lotID, session.Entry, "34RPT005", gunBas.Add(35*time.Hour), session.OutcomeOpened)
	disariCikis := opsOlay(t, env.pool, env.lotID, session.Exit, "34RPT005", gunBas.Add(37*time.Hour), session.OutcomeClosed)
	disariSess, err := store.InsertSession(ctx, env.pool, env.lotID, "34RPT005", disariGiris, gunBas.Add(35*time.Hour), env.tariffID)
	if err != nil {
		t.Fatalf("gun disi oturum acilamadi: %v", err)
	}
	if err := store.SetEventOutcome(ctx, env.pool, disariCikis, session.OutcomeClosed, disariSess); err != nil {
		t.Fatalf("gun disi cikis baglanamadi: %v", err)
	}
	if err := store.CloseSession(ctx, env.pool, disariSess, disariCikis, gunBas.Add(37*time.Hour), 999, nil); err != nil {
		t.Fatalf("gun disi oturum kapatilamadi: %v", err)
	}

	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "")
	_, gorevli := opsToken(t, env.h, env.pool, auth.RoleAttendant, "")
	_, muhasebeci := opsToken(t, env.h, env.pool, auth.RoleAccountant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	rapor := fmt.Sprintf("/reports/daily?day=2026-03-01&lot_id=%d", env.lotID)
	rec := opsDo(t, env.h, http.MethodGet, rapor, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, rapor, sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, rapor, gorevli, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("gorevli durum = %d, beklenen 403", rec.Code)
	}

	for ad, token := range map[string]string{"muhasebeci": muhasebeci, "mudur": mudur, "yonetici": yonetici} {
		t.Run(ad, func(t *testing.T) {
			rec := opsDo(t, env.h, http.MethodGet, rapor, token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)
			beklenen := map[string]float64{
				"entries":            1,
				"exits":              1,
				"closed_sessions":    1,
				"total_amount_kurus": 1500,
				"collected_kurus":    1000,
				"open_debt_kurus":    1500,
				"open_reviews":       1,
			}
			for alan, deger := range beklenen {
				if body[alan] != deger {
					t.Fatalf("%s = %v, beklenen %v (govde: %v)", alan, body[alan], deger, body)
				}
			}
			if body["day"] != "2026-03-01" || int64(body["lot_id"].(float64)) != env.lotID {
				t.Fatalf("rapor kimligi yanlis: %v", body)
			}
		})
	}

	rec = opsDo(t, env.h, http.MethodGet, "/reports/daily?day=bozuk", muhasebeci, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk gun durum = %d, beklenen 400", rec.Code)
	}
	assertErrorBody(t, rec, "invalid_day")
	rec = opsDo(t, env.h, http.MethodGet, "/reports/daily?day=2026-03-01&lot_id=abc", muhasebeci, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk lot durum = %d, beklenen 400", rec.Code)
	}
}

func TestOpsEventsRecent(t *testing.T) {
	env := opsHazirla(t, 10)
	ikinciLot, _ := opsLotEkle(t, env.pool, 10)
	at := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	idler := make([]int64, 0, 5)
	for i := 0; i < 5; i++ {
		idler = append(idler, opsOlay(t, env.pool, env.lotID, session.Entry, fmt.Sprintf("34EVT00%d", i),
			at.Add(time.Duration(i)*time.Minute), session.OutcomeOpened))
	}
	ikinciID := opsOlay(t, env.pool, ikinciLot, session.Exit, "34EVT999", at, session.OutcomeOrphanExit)

	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "")
	_, gorevli := opsToken(t, env.h, env.pool, auth.RoleAttendant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	rec := opsDo(t, env.h, http.MethodGet, "/events/recent", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, "/events/recent", sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d, beklenen 403", rec.Code)
	}

	for ad, token := range map[string]string{"gorevli": gorevli, "mudur": mudur, "yonetici": yonetici} {
		t.Run(ad, func(t *testing.T) {
			rec := opsDo(t, env.h, http.MethodGet, fmt.Sprintf("/events/recent?lot_id=%d&limit=2", env.lotID), token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)
			items, _ := body["items"].([]any)
			if len(items) != 2 {
				t.Fatalf("items = %d, beklenen 2", len(items))
			}
			birinci := int64(items[0].(map[string]any)["id"].(float64))
			ikinci := int64(items[1].(map[string]any)["id"].(float64))
			if birinci != idler[4] || ikinci != idler[3] {
				t.Fatalf("olay sirasi yanlis: %d, %d", birinci, ikinci)
			}
			if items[0].(map[string]any)["outcome"] != "opened" ||
				items[0].(map[string]any)["plate_norm"] != "34EVT004" {
				t.Fatalf("olay alanlari yanlis: %v", items[0])
			}
			nextID, ok := body["next_before_id"].(float64)
			if !ok || int64(nextID) != ikinci {
				t.Fatalf("next_before_id = %v, beklenen %d", body["next_before_id"], ikinci)
			}

			rec = opsDo(t, env.h, http.MethodGet,
				fmt.Sprintf("/events/recent?lot_id=%d&before_id=%d&limit=2", env.lotID, int64(nextID)), token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("ikinci sayfa durum = %d", rec.Code)
			}
			sayfa2 := decodeJSON(t, rec)
			items2, _ := sayfa2["items"].([]any)
			if len(items2) != 2 {
				t.Fatalf("ikinci sayfa items = %d, beklenen 2", len(items2))
			}
			for _, raw := range items2 {
				if int64(raw.(map[string]any)["id"].(float64)) >= int64(nextID) {
					t.Fatalf("keyset ihlali: %v >= %d", raw.(map[string]any)["id"], int64(nextID))
				}
			}
		})
	}

	rec = opsDo(t, env.h, http.MethodGet, fmt.Sprintf("/events/recent?lot_id=%d&limit=100", ikinciLot), gorevli, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lot filtresi durum = %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	items, _ := body["items"].([]any)
	bulundu := false
	for _, raw := range items {
		oge := raw.(map[string]any)
		if int64(oge["id"].(float64)) == ikinciID {
			bulundu = true
		}
		if int64(oge["lot_id"].(float64)) != ikinciLot {
			t.Fatalf("lot filtresi ihlali: %v", oge["lot_id"])
		}
	}
	if !bulundu {
		t.Fatalf("ikinci lot olayi listede yok")
	}

	rec = opsDo(t, env.h, http.MethodGet, "/events/recent?limit=0", gorevli, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk limit durum = %d, beklenen 400", rec.Code)
	}
}

func TestOpsReviewsUclari(t *testing.T) {
	env := opsHazirla(t, 10)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)

	giris := opsOlay(t, env.pool, env.lotID, session.Entry, "34REV001", at, session.OutcomeOpened)
	oturumID, err := store.InsertSession(ctx, env.pool, env.lotID, "34REV001", giris, at, env.tariffID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}

	olay1 := opsOlay(t, env.pool, env.lotID, session.Entry, "34REV001", at.Add(time.Hour), session.OutcomeReview)
	inceleme1, err := store.InsertReview(ctx, env.pool, olay1, env.lotID, "34REV001", session.Review{
		Reason:             session.ReasonPossibleDuplicateEntry,
		SuggestedSessionID: oturumID,
	})
	if err != nil {
		t.Fatalf("inceleme eklenemedi: %v", err)
	}
	olay2 := opsOlay(t, env.pool, env.lotID, session.Entry, "34REV002", at.Add(2*time.Hour), session.OutcomeReview)
	inceleme2, err := store.InsertReview(ctx, env.pool, olay2, env.lotID, "34REV002", session.Review{
		Reason:             session.ReasonAmbiguous,
		SuggestedSessionID: oturumID,
	})
	if err != nil {
		t.Fatalf("inceleme eklenemedi: %v", err)
	}
	olay3 := opsOlay(t, env.pool, env.lotID, session.Entry, "34REV003", at.Add(3*time.Hour), session.OutcomeReview)
	inceleme3, err := store.InsertReview(ctx, env.pool, olay3, env.lotID, "34REV003", session.Review{Reason: session.ReasonAmbiguous})
	if err != nil {
		t.Fatalf("inceleme eklenemedi: %v", err)
	}

	baskaLot, baskaTarife := opsLotEkle(t, env.pool, 10)
	baskaGiris := opsOlay(t, env.pool, baskaLot, session.Entry, "34REV009", at, session.OutcomeOpened)
	baskaOturum, err := store.InsertSession(ctx, env.pool, baskaLot, "34REV009", baskaGiris, at, baskaTarife)
	if err != nil {
		t.Fatalf("baska lot oturumu acilamadi: %v", err)
	}

	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "")
	_, gorevli := opsToken(t, env.h, env.pool, auth.RoleAttendant, "")
	_, muhasebeci := opsToken(t, env.h, env.pool, auth.RoleAccountant, "")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")
	_, yonetici := opsToken(t, env.h, env.pool, auth.RoleAdmin, "")

	rec := opsDo(t, env.h, http.MethodGet, "/reviews", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yetkisiz durum = %d, beklenen 401", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, "/reviews", sofor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d, beklenen 403", rec.Code)
	}
	for ad, token := range map[string]string{"gorevli": gorevli, "mudur": mudur, "yonetici": yonetici} {
		t.Run("liste-"+ad, func(t *testing.T) {
			rec := opsDo(t, env.h, http.MethodGet, "/reviews?status=open&limit=100", token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)
			items, _ := body["items"].([]any)
			var bizim map[string]any
			for _, raw := range items {
				oge := raw.(map[string]any)
				if int64(oge["id"].(float64)) == inceleme1 {
					bizim = oge
				}
			}
			if bizim == nil {
				t.Fatalf("inceleme listede yok")
			}
			if bizim["plate_raw"] != "34REV001" || bizim["status"] != "open" ||
				int64(bizim["suggested_session_id"].(float64)) != oturumID {
				t.Fatalf("inceleme alanlari yanlis: %v", bizim)
			}
		})
	}

	rec = opsDo(t, env.h, http.MethodGet, fmt.Sprintf("/reviews?status=open&after_id=%d&limit=1", inceleme1), gorevli, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("keyset durum = %d", rec.Code)
	}
	sayfa := decodeJSON(t, rec)
	items, _ := sayfa["items"].([]any)
	if len(items) != 1 || int64(items[0].(map[string]any)["id"].(float64)) != inceleme2 {
		t.Fatalf("keyset items yanlis: %v", items)
	}
	if int64(sayfa["next_after_id"].(float64)) != inceleme2 {
		t.Fatalf("next_after_id yanlis: %v", sayfa["next_after_id"])
	}

	resolvePath := func(id int64) string { return fmt.Sprintf("/reviews/%d/resolve", id) }
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme1), sofor, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor cozumleme durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme1), muhasebeci, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("muhasebeci cozumleme durum = %d, beklenen 403", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme3), gorevli, map[string]any{"action": "bilinmeyen"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("gecersiz eylem durum = %d, beklenen 400", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme3), gorevli,
		map[string]any{"action": "link_session", "session_id": baskaOturum})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("farkli lot oturumu durum = %d, beklenen 400", rec.Code)
	}
	assertErrorBody(t, rec, "session_lot_mismatch")
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme3), gorevli,
		map[string]any{"action": "link_session", "session_id": 999999999})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan oturum durum = %d, beklenen 404", rec.Code)
	}

	denetimOnce := auditSayisi(t, env.pool, "review.resolve")
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme1), gorevli, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["status"] != "resolved" {
		t.Fatalf("dismiss yaniti yanlis: %v", body)
	}
	var status string
	var resolvedAt *time.Time
	if err := env.pool.QueryRow(ctx,
		`SELECT status, resolved_at FROM "ya-review_items" WHERE id = $1`, inceleme1,
	).Scan(&status, &resolvedAt); err != nil {
		t.Fatalf("inceleme okunamadi: %v", err)
	}
	if status != "resolved" || resolvedAt == nil {
		t.Fatalf("dismiss sonrasi durum = %q, resolved_at %v", status, resolvedAt)
	}
	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme1), gorevli, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("ikinci dismiss durum = %d, beklenen 409", rec.Code)
	}
	assertErrorBody(t, rec, "review_not_open")

	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme2), yonetici,
		map[string]any{"action": "link_session", "session_id": oturumID})
	if rec.Code != http.StatusOK {
		t.Fatalf("link_session durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var bagli *int64
	if err := env.pool.QueryRow(ctx,
		`SELECT session_id FROM "ya-plate_events" WHERE id = $1`, olay2).Scan(&bagli); err != nil {
		t.Fatalf("olay okunamadi: %v", err)
	}
	if bagli == nil || *bagli != oturumID {
		t.Fatalf("olay oturuma baglanmadi: %v", bagli)
	}
	if sonra := auditSayisi(t, env.pool, "review.resolve"); sonra < denetimOnce+2 {
		t.Fatalf("review.resolve denetim kaydi artmadi: %d -> %d", denetimOnce, sonra)
	}

	rec = opsDo(t, env.h, http.MethodPost, resolvePath(inceleme3), gorevli, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusOK {
		t.Fatalf("ucuncu dismiss durum = %d", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodPost, "/reviews/999999999/resolve", gorevli, map[string]any{"action": "dismiss"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan inceleme durum = %d, beklenen 404", rec.Code)
	}
	rec = opsDo(t, env.h, http.MethodGet, "/reviews?status=bozuk", gorevli, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk durum filtresi = %d, beklenen 400", rec.Code)
	}
}

func TestOpsReservationIdempotency(t *testing.T) {
	env := opsHazirla(t, 5)
	_, sofor := opsToken(t, env.h, env.pool, auth.RoleDriver, "34IDM123")
	_, mudur := opsToken(t, env.h, env.pool, auth.RoleManager, "")

	gonder := func(token, anahtar string, body map[string]any) *httptest.ResponseRecorder {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("govde hazirlanamadi: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/reservations", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if anahtar != "" {
			req.Header.Set("Idempotency-Key", anahtar)
		}
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		return rec
	}

	govde := map[string]any{"lot_id": env.lotID, "plate": "34 IDM 123"}
	ilk := gonder(sofor, "http-rez-idem-1", govde)
	if ilk.Code != http.StatusCreated {
		t.Fatalf("ilk rezervasyon durum = %d; govde: %s", ilk.Code, ilk.Body.String())
	}
	ilkGovde := decodeJSON(t, ilk)
	if ilkGovde["replayed"] != false {
		t.Fatalf("ilk replayed = %v, beklenen false", ilkGovde["replayed"])
	}
	ilkID := int64(ilkGovde["id"].(float64))

	tekrar := gonder(sofor, "http-rez-idem-1", govde)
	if tekrar.Code != http.StatusOK {
		t.Fatalf("tekrar rezervasyon durum = %d; govde: %s", tekrar.Code, tekrar.Body.String())
	}
	tekrarGovde := decodeJSON(t, tekrar)
	if tekrarGovde["replayed"] != true {
		t.Fatalf("tekrar replayed = %v, beklenen true", tekrarGovde["replayed"])
	}
	if int64(tekrarGovde["id"].(float64)) != ilkID {
		t.Fatalf("tekrar kimligi = %v, beklenen %d", tekrarGovde["id"], ilkID)
	}

	farkli := gonder(mudur, "http-rez-idem-1", map[string]any{"lot_id": env.lotID, "plate": "34IDM999"})
	if farkli.Code != http.StatusConflict {
		t.Fatalf("farkli govde durum = %d; govde: %s", farkli.Code, farkli.Body.String())
	}
	assertErrorBody(t, farkli, "idempotency_conflict")

	uzun := gonder(sofor, strings.Repeat("a", 256), govde)
	if uzun.Code != http.StatusBadRequest {
		t.Fatalf("uzun anahtar durum = %d, beklenen 400", uzun.Code)
	}
}
