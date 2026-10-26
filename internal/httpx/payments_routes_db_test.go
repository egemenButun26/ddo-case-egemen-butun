package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"parkakil/internal/auth"
	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/paymentsvc"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

const t4Secret = "test-provider-secret-0123456789"

var t4PlakaSayac atomic.Int64

func t4Plaka() string {
	n := t4PlakaSayac.Add(1)
	return fmt.Sprintf("%02d%c%c%04d", 10+n%70, 'A'+(n/26)%26, 'A'+n%26, n%10000)
}

func hash32(b byte) [32]byte {
	var h [32]byte
	for i := range h {
		h[i] = b
	}
	return h
}

type chargeTrap struct {
	mu     chan struct{}
	items  []providerChargeRequest
	notify bool
	secret string
}

func newChargeTrap(notify bool) *chargeTrap {
	return &chargeTrap{mu: make(chan struct{}, 1), notify: notify, secret: t4Secret}
}

func (c *chargeTrap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req providerChargeRequest
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)
	c.mu <- struct{}{}
	c.items = append(c.items, req)
	<-c.mu
	if c.notify {
		out, _ := json.Marshal(map[string]any{
			"provider_ref": req.ProviderRef,
			"outcome":      "succeeded",
			"amount_kurus": req.AmountKurus,
			"provider_at":  time.Now().UTC(),
		})
		nreq, err := http.NewRequest(http.MethodPost, req.CallbackURL, bytes.NewReader(out))
		if err == nil {
			nreq.Header.Set("Content-Type", "application/json")
			nreq.Header.Set(providerSignature, signProviderBody(c.secret, out))
			resp, err := http.DefaultClient.Do(nreq)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"accepted":true}`))
}

func (c *chargeTrap) charges() []providerChargeRequest {
	c.mu <- struct{}{}
	defer func() { <-c.mu }()
	out := make([]providerChargeRequest, len(c.items))
	copy(out, c.items)
	return out
}

type t4Ortam struct {
	t        *testing.T
	pool     *pgxpool.Pool
	svc      *paymentsvc.Service
	handler  http.Handler
	apiURL   string
	lotID    int64
	tariffID string
	trap     *chargeTrap
	apiSrv   *httptest.Server
	provSrv  *httptest.Server
}

func t4Kur(t *testing.T, freeMinutes int, band tariff.Band, notify bool) *t4Ortam {
	t.Helper()
	pool := testdb.Open(t)
	lotID := testdb.NewLot(t, pool, 100)
	v := tariff.Version{
		ID:          benzersiz("tarife"),
		ValidFrom:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:    time.UTC,
		FreeMinutes: freeMinutes,
		Weekday: tariff.DayRule{
			Name:  "hafta_ici",
			Bands: []tariff.Band{band},
		},
	}
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	svc, err := paymentsvc.New(pool, paymentsvc.Config{
		ProviderTimeout: time.Minute,
		Thresholds:      plate.DefaultThresholds,
	})
	if err != nil {
		t.Fatalf("odeme servisi: %v", err)
	}
	events, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		t.Fatalf("oturum servisi: %v", err)
	}

	trap := newChargeTrap(notify)
	provSrv := httptest.NewServer(trap)
	t.Cleanup(provSrv.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinleyici: %v", err)
	}
	apiURL := "http://" + ln.Addr().String()
	handler := NewRouter(Deps{
		Logger:        discardLogger(),
		Pinger:        pool,
		PingTimeout:   time.Second,
		CameraKey:     testKameraAnahtari,
		Events:        events,
		DB:            pool,
		SessionTTL:    time.Hour,
		Payments:      svc,
		ProviderURL:   provSrv.URL,
		WebhookSecret: t4Secret,
		PublicAPIURL:  apiURL,
	})
	apiSrv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	apiSrv.Start()
	t.Cleanup(apiSrv.Close)

	return &t4Ortam{
		t: t, pool: pool, svc: svc, handler: handler, apiURL: apiURL,
		lotID: lotID, tariffID: v.ID, trap: trap, apiSrv: apiSrv, provSrv: provSrv,
	}
}

func (o *t4Ortam) oturum(plaka string, entered time.Time) int64 {
	o.t.Helper()
	ev := session.Event{
		LotID:         o.lotID,
		CameraID:      benzersiz("cam"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Entry,
		PlateRaw:      plaka,
		CapturedAt:    entered,
	}
	eventID, err := store.InsertEvent(context.Background(), o.pool, ev, plate.Normalize(plaka), session.OutcomeOpened, 0)
	if err != nil {
		o.t.Fatalf("giris olayi: %v", err)
	}
	id, err := store.InsertSession(context.Background(), o.pool, o.lotID, plate.Normalize(plaka), eventID, entered, o.tariffID)
	if err != nil {
		o.t.Fatalf("oturum: %v", err)
	}
	return id
}

func (o *t4Ortam) odeme(sessionID int64, amount tariff.Kurus, paidAt time.Time) int64 {
	o.t.Helper()
	p := payment.Payment{
		ProviderRef: benzersiz("prv"),
		Amount:      amount,
		Status:      payment.StatusSucceeded,
		RequestedAt: paidAt.Add(-time.Minute),
		PaidAt:      paidAt,
		ConfirmedAt: paidAt.Add(time.Second),
	}
	id, err := store.InsertPayment(context.Background(), o.pool, sessionID, benzersiz("idem"), hash32(1), p)
	if err != nil {
		o.t.Fatalf("odeme: %v", err)
	}
	return id
}

func (o *t4Ortam) token(role auth.Role, plaka *string) string {
	o.t.Helper()
	hash, err := auth.HashPasswordCost("test-parola-123", bcrypt.MinCost)
	if err != nil {
		o.t.Fatalf("parola: %v", err)
	}
	id, err := store.CreateUser(context.Background(), o.pool, store.CreateUserParams{
		Email:        strings.ToLower(benzersiz("t4")) + "@test.local",
		Name:         "T4 Kullanici",
		Role:         role,
		PasswordHash: hash,
		Plate:        plaka,
	})
	if err != nil {
		o.t.Fatalf("kullanici: %v", err)
	}
	token := benzersiz("tok")
	if _, err := store.CreateAuthSession(context.Background(), o.pool, id, auth.HashToken(token), time.Now().Add(time.Hour)); err != nil {
		o.t.Fatalf("oturum: %v", err)
	}
	return token
}

func (o *t4Ortam) istek(method, path string, headers map[string]string, body any) *httptest.ResponseRecorder {
	o.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			o.t.Fatalf("govde: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	return rec
}

func (o *t4Ortam) cihaz(method, path string, body any) *httptest.ResponseRecorder {
	return o.istek(method, path, map[string]string{cameraKeyHeader: testKameraAnahtari}, body)
}

func (o *t4Ortam) bildirim(t *testing.T, ref, sonuc string, amount tariff.Kurus, at time.Time) *httptest.ResponseRecorder {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"provider_ref": ref,
		"outcome":      sonuc,
		"amount_kurus": amount,
		"provider_at":  at,
	})
	if err != nil {
		t.Fatalf("bildirim govdesi: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/provider/notifications", bytes.NewReader(out))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(providerSignature, signProviderBody(t4Secret, out))
	rec := httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	return rec
}

func t4JSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("govde JSON degil (%q): %v", rec.Body.String(), err)
	}
	return m
}

func t4Sayi(m map[string]any, key string) int64 {
	v, _ := m[key].(float64)
	return int64(v)
}

func t4SonOdemeID(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(max(id), 0) FROM "ya-payments"`).Scan(&id); err != nil {
		t.Fatalf("son odeme: %v", err)
	}
	return id
}

func t4AuditSayisi(t *testing.T, pool *pgxpool.Pool, action, entityID string) int {
	t.Helper()
	var sayi int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-audit_log" WHERE action = $1 AND entity_id = $2`, action, entityID).Scan(&sayi); err != nil {
		t.Fatalf("denetim sayisi: %v", err)
	}
	return sayi
}

func TestT4HttpKioskQuote(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	entered := time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Microsecond)
	plaka := t4Plaka()
	ham := plaka[:2] + " " + plaka[2:3] + " " + plaka[3:]
	sessID := o.oturum(plaka, entered)

	rec := o.cihaz(http.MethodGet, fmt.Sprintf("/kiosk/quote?lot_id=%d&plate=%s", o.lotID, url.QueryEscape(ham)), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := t4JSON(t, rec)
	if t4Sayi(body, "session_id") != sessID {
		t.Fatalf("session_id = %v", body["session_id"])
	}
	if body["plate"] != plaka {
		t.Fatalf("plate = %v", body["plate"])
	}
	if t4Sayi(body, "total_kurus") != 1000 || t4Sayi(body, "due_kurus") != 1000 || t4Sayi(body, "paid_kurus") != 0 {
		t.Fatalf("tutar dokumu = %v", body)
	}
	if body["permit_expires_at"] != nil {
		t.Fatalf("permit_expires_at = %v", body["permit_expires_at"])
	}

	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/kiosk/quote?lot_id=%d&plate=06ZZZ99", o.lotID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan oturum durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "session_not_found")

	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/kiosk/quote?lot_id=%d", o.lotID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("plakasiz durum = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/kiosk/quote?lot_id=%d&plate=%s", o.lotID, plaka), nil)
	rec = httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anahtarsiz durum = %d", rec.Code)
	}
}

func TestT4HttpKioskPaymentIdempotency(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	entered := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Microsecond)
	sessID := o.oturum(t4Plaka(), entered)
	anahtar := benzersiz("idem")

	govde := map[string]any{"session_id": sessID, "amount_kurus": 500}
	rec := o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: anahtar}, govde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("yeni odeme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := t4JSON(t, rec)
	if !strings.HasPrefix(fmt.Sprint(ilk["provider_ref"]), "prv-") {
		t.Fatalf("provider_ref = %v", ilk["provider_ref"])
	}
	if ilk["status"] != "pending" || ilk["replayed"] != false {
		t.Fatalf("ilk yanit = %v", ilk)
	}
	charges := o.trap.charges()
	if len(charges) != 1 || charges[0].ProviderRef != fmt.Sprint(ilk["provider_ref"]) {
		t.Fatalf("saglayici istegi = %+v", charges)
	}
	if !strings.HasSuffix(charges[0].CallbackURL, "/provider/notifications") {
		t.Fatalf("callback = %q", charges[0].CallbackURL)
	}

	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: anahtar}, govde)
	if rec.Code != http.StatusOK {
		t.Fatalf("tekrar durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	tekrar := t4JSON(t, rec)
	if tekrar["replayed"] != true || tekrar["payment_id"] != ilk["payment_id"] {
		t.Fatalf("tekrar yanit = %v", tekrar)
	}
	if len(o.trap.charges()) != 1 {
		t.Fatal("tekrar istekte saglayici yeniden cagrildi")
	}

	farkli := map[string]any{"session_id": sessID, "amount_kurus": 400}
	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: anahtar}, farkli)
	if rec.Code != http.StatusConflict {
		t.Fatalf("farkli govde durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "idempotency_conflict")

	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari}, govde)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("anahtarsiz durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "idempotency_key_required")

	yeni := map[string]any{"session_id": sessID, "amount_kurus": 400}
	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: benzersiz("idem")}, yeni)
	if rec.Code != http.StatusConflict {
		t.Fatalf("tutar uyusmazligi durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "amount_mismatch")

	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: benzersiz("idem")}, map[string]any{"session_id": sessID, "amount_kurus": 500})
	if rec.Code != http.StatusConflict {
		t.Fatalf("bekleyen odeme durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "payment_pending")

	rec = o.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: benzersiz("idem")}, map[string]any{"session_id": 1 << 50, "amount_kurus": 500})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan oturum durum = %d", rec.Code)
	}

	o2 := t4Kur(t, 120, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	ucretsiz := o2.oturum(t4Plaka(), time.Now().UTC().Add(-30*time.Minute))
	rec = o2.istek(http.MethodPost, "/kiosk/payments", map[string]string{cameraKeyHeader: testKameraAnahtari, idempotencyHeader: benzersiz("idem")}, map[string]any{"session_id": ucretsiz, "amount_kurus": 100})
	if rec.Code != http.StatusConflict {
		t.Fatalf("ucretsiz oturum durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "nothing_due")
}

func TestT4HttpProviderNotifications(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	entered := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Microsecond)
	sessID := o.oturum(t4Plaka(), entered)
	now := time.Now().UTC()
	ref := benzersiz("prv")
	res, err := o.svc.CreatePayment(context.Background(), paymentsvc.CreateReq{
		SessionID:      sessID,
		Amount:         500,
		IdempotencyKey: benzersiz("idem"),
		RequestHash:    hash32(2),
		ProviderRef:    ref,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}
	paymentID := res.PaymentID

	body, _ := json.Marshal(map[string]any{
		"provider_ref": ref,
		"outcome":      "succeeded",
		"amount_kurus": 500,
		"provider_at":  now,
	})
	req := httptest.NewRequest(http.MethodPost, "/provider/notifications", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("imzasiz durum = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/provider/notifications", bytes.NewReader(body))
	req.Header.Set(providerSignature, "sha256=deadbeef")
	rec = httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("yanlis imza durum = %d", rec.Code)
	}

	rec = o.bildirim(t, ref, "succeeded", 500, now)
	if rec.Code != http.StatusOK {
		t.Fatalf("dogru bildirim durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := t4JSON(t, rec)
	if ilk["status"] != "succeeded" || ilk["duplicate"] != false {
		t.Fatalf("bildirim yaniti = %v", ilk)
	}

	rec = o.bildirim(t, ref, "succeeded", 500, now)
	if rec.Code != http.StatusOK {
		t.Fatalf("yinelenen bildirim durum = %d", rec.Code)
	}
	tekrar := t4JSON(t, rec)
	if tekrar["duplicate"] != true || tekrar["payment_id"] != ilk["payment_id"] {
		t.Fatalf("yinelenen yanit = %v", tekrar)
	}
	var sayi int
	if err := o.pool.QueryRow(context.Background(), `SELECT count(*) FROM "ya-payments" WHERE id = $1`, paymentID).Scan(&sayi); err != nil {
		t.Fatalf("odeme sayisi: %v", err)
	}
	if sayi != 1 {
		t.Fatalf("odeme satiri = %d", sayi)
	}
	if n := t4AuditSayisi(t, o.pool, "payment.notified", fmt.Sprint(paymentID)); n < 2 {
		t.Fatalf("payment.notified denetimi = %d", n)
	}
	if n := t4AuditSayisi(t, o.pool, "payment.created", fmt.Sprint(paymentID)); n != 0 {
		t.Fatalf("servis disi odeme icin payment.created = %d", n)
	}

	rec = o.bildirim(t, ref, "failed", 500, now)
	if rec.Code != http.StatusConflict {
		t.Fatalf("celisen bildirim durum = %d", rec.Code)
	}

	// Gec onay + fazla odeme: iki odeme yazilir, biri zaman asimina ugrar,
	// gec onaydan sonra ikinci odeme basarili olunca fazla tutar iade edilir.
	o2 := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	sess2 := o2.oturum(t4Plaka(), time.Now().UTC().Add(-30*time.Minute))
	istekZamani := time.Now().UTC().Add(-10 * time.Minute)
	gec, err := o2.svc.CreatePayment(context.Background(), paymentsvc.CreateReq{
		SessionID: sess2, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(3), ProviderRef: benzersiz("prv"), Now: istekZamani,
	})
	if err != nil {
		t.Fatalf("gec odeme: %v", err)
	}
	if _, err := o2.svc.ExpirePending(context.Background(), istekZamani.Add(2*time.Minute)); err != nil {
		t.Fatalf("zaman asimi: %v", err)
	}
	ikinci, err := o2.svc.CreatePayment(context.Background(), paymentsvc.CreateReq{
		SessionID: sess2, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(4), ProviderRef: benzersiz("prv"), Now: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("ikinci odeme: %v", err)
	}
	gecOnay := time.Now().UTC()
	rec = o2.bildirim(t, gec.Payment.ProviderRef, "succeeded", 500, gecOnay)
	if rec.Code != http.StatusOK {
		t.Fatalf("gec onay durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	if t4JSON(t, rec)["late_approval"] != true {
		t.Fatalf("gec onay isaretlenmedi: %s", rec.Body.String())
	}
	p2At := time.Now().UTC()
	rec = o2.bildirim(t, ikinci.Payment.ProviderRef, "succeeded", 500, p2At)
	if rec.Code != http.StatusOK {
		t.Fatalf("fazla odeme bildirimi durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	faz := t4JSON(t, rec)
	if faz["refund_required"] != true {
		t.Fatalf("iade gerekmedi: %v", faz)
	}
	iadeID := t4Sayi(faz, "refund_id")
	if iadeID == 0 {
		t.Fatal("iade kimligi yok")
	}
	rec = o2.bildirim(t, ikinci.Payment.ProviderRef, "succeeded", 500, p2At)
	if rec.Code != http.StatusOK {
		t.Fatalf("yinelenen fazla odeme durum = %d", rec.Code)
	}
	if t4Sayi(t4JSON(t, rec), "refund_id") != iadeID {
		t.Fatal("yinelenen bildirimde iade degisti")
	}
	var iadeSayi int
	if err := o2.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-refunds" WHERE payment_id = $1 AND reason = 'overpaid'`, ikinci.PaymentID).Scan(&iadeSayi); err != nil {
		t.Fatalf("iade sayisi: %v", err)
	}
	if iadeSayi != 1 {
		t.Fatalf("otomatik iade kaydi = %d, beklenen 1", iadeSayi)
	}
	if n := t4AuditSayisi(t, o2.pool, "refund.created", fmt.Sprint(iadeID)); n != 1 {
		t.Fatalf("refund.created denetimi = %d", n)
	}
	_ = gec
}

func TestT4HttpGateDecision(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 10, UnitPrice: 100}, false)
	simdi := time.Now().UTC()
	entered := simdi.Add(-25 * time.Minute).Truncate(time.Microsecond)

	plaka1 := t4Plaka()
	sess := o.oturum(plaka1, entered)
	rec := o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka1), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	karar := t4JSON(t, rec)
	if karar["action"] != "deny" || karar["reason"] != "payment_required" {
		t.Fatalf("odemesiz karar = %v", karar)
	}

	o.odeme(sess, 300, simdi.Add(-time.Minute))
	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka1), nil)
	karar = t4JSON(t, rec)
	if karar["action"] != "open" || karar["reason"] != "paid" {
		t.Fatalf("tam odeme karari = %v", karar)
	}

	plaka2 := t4Plaka()
	sess2 := o.oturum(plaka2, entered)
	o.odeme(sess2, 200, simdi.Add(-10*time.Minute))
	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka2), nil)
	karar = t4JSON(t, rec)
	if karar["action"] != "open" || karar["reason"] != "permit_valid" {
		t.Fatalf("gecerli izin karari = %v", karar)
	}
	if karar["permit_expires_at"] == nil {
		t.Fatal("permit_expires_at bos")
	}

	plaka3 := t4Plaka()
	sess3 := o.oturum(plaka3, entered)
	o.odeme(sess3, 100, simdi.Add(-20*time.Minute))
	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka3), nil)
	karar = t4JSON(t, rec)
	if karar["action"] != "deny" || karar["reason"] != "permit_expired" {
		t.Fatalf("suresi dolan izin karari = %v", karar)
	}

	plaka4 := t4Plaka()
	sess4 := o.oturum(plaka4, entered)
	pending := payment.Payment{
		ProviderRef: benzersiz("prv"), Amount: 300, Status: payment.StatusPending,
		RequestedAt: simdi.Add(-time.Minute),
	}
	if _, err := store.InsertPayment(context.Background(), o.pool, sess4, benzersiz("idem"), hash32(5), pending); err != nil {
		t.Fatalf("bekleyen odeme: %v", err)
	}
	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka4), nil)
	karar = t4JSON(t, rec)
	if karar["action"] != "deny" || karar["reason"] != "payment_pending" {
		t.Fatalf("bekleyen odeme karari = %v", karar)
	}

	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=06ZZZ99", o.lotID), nil)
	karar = t4JSON(t, rec)
	if karar["action"] != "deny" || karar["reason"] != "no_match" {
		t.Fatalf("eslesmeyen karar = %v", karar)
	}
}

func TestT4HttpPaymentsPaginationAndRoles(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	taban := t4SonOdemeID(t, o.pool)
	for i := 0; i < 3; i++ {
		sess := o.oturum(t4Plaka(), time.Now().UTC().Add(-time.Hour))
		o.odeme(sess, tariff.Kurus(100+i), time.Now().UTC().Add(-time.Minute))
	}

	muhasebe := o.token(auth.RoleAccountant, nil)
	rec := o.istek(http.MethodGet, fmt.Sprintf("/payments?after_id=%d&limit=2", taban), map[string]string{"Authorization": "Bearer " + muhasebe}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := t4JSON(t, rec)
	items, _ := ilk["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("ilk sayfa = %d", len(items))
	}
	next := t4Sayi(ilk, "next_after_id")
	if next == 0 {
		t.Fatal("next_after_id bos")
	}
	rec = o.istek(http.MethodGet, fmt.Sprintf("/payments?after_id=%d&limit=2", next), map[string]string{"Authorization": "Bearer " + muhasebe}, nil)
	ikinci := t4JSON(t, rec)
	if len(ikinci["items"].([]any)) != 1 {
		t.Fatalf("ikinci sayfa = %v", ikinci)
	}

	rec = o.istek(http.MethodGet, "/payments?limit=101", map[string]string{"Authorization": "Bearer " + muhasebe}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("gecersiz limit durum = %d", rec.Code)
	}

	sofor := o.token(auth.RoleDriver, nil)
	rec = o.istek(http.MethodGet, "/payments", map[string]string{"Authorization": "Bearer " + sofor}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor durum = %d", rec.Code)
	}
	rec = o.istek(http.MethodGet, "/payments", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anahtarsiz durum = %d", rec.Code)
	}
}

func TestT4HttpRefunds(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	sess := o.oturum(t4Plaka(), time.Now().UTC().Add(-time.Hour))
	odemeID := o.odeme(sess, 1000, time.Now().UTC().Add(-time.Minute))

	muhasebe := o.token(auth.RoleAccountant, nil)
	basliklar := map[string]string{"Authorization": "Bearer " + muhasebe, idempotencyHeader: benzersiz("iade")}
	govde := map[string]any{"payment_id": odemeID, "amount_kurus": 400, "reason": "manual"}
	rec := o.istek(http.MethodPost, "/refunds", basliklar, govde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("iade durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	ilk := t4JSON(t, rec)
	iadeID := t4Sayi(ilk, "id")

	rec = o.istek(http.MethodPost, "/refunds", basliklar, govde)
	if rec.Code != http.StatusOK {
		t.Fatalf("tekrar iade durum = %d", rec.Code)
	}
	if t4Sayi(t4JSON(t, rec), "id") != iadeID {
		t.Fatal("tekrar iade kimligi degisti")
	}

	rec = o.istek(http.MethodPost, "/refunds", basliklar, map[string]any{"payment_id": odemeID, "amount_kurus": 300, "reason": "manual"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("celisen anahtar durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "idempotency_conflict")

	rec = o.istek(http.MethodPost, "/refunds", map[string]string{"Authorization": "Bearer " + muhasebe, idempotencyHeader: benzersiz("iade")},
		map[string]any{"payment_id": odemeID, "amount_kurus": 700, "reason": "manual"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("fazla iade durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "refund_exceeds_payment")

	rec = o.istek(http.MethodPost, "/refunds", map[string]string{"Authorization": "Bearer " + muhasebe}, govde)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("anahtarsiz iade durum = %d", rec.Code)
	}
	assertErrorBody(t, rec, "idempotency_key_required")

	sofor := o.token(auth.RoleDriver, nil)
	rec = o.istek(http.MethodPost, "/refunds", map[string]string{"Authorization": "Bearer " + sofor, idempotencyHeader: benzersiz("iade")}, govde)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor iade durum = %d", rec.Code)
	}
	if n := t4AuditSayisi(t, o.pool, "refund.created", fmt.Sprint(iadeID)); n != 1 {
		t.Fatalf("iade denetimi = %d", n)
	}
}

func TestT4HttpDebts(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	kendi := t4Plaka()
	sess := o.oturum(kendi, time.Now().UTC().Add(-2*time.Hour))
	if _, err := store.InsertDebt(context.Background(), o.pool, sess, o.lotID, kendi, 700); err != nil {
		t.Fatalf("borc: %v", err)
	}

	sofor := o.token(auth.RoleDriver, &kendi)
	rec := o.istek(http.MethodGet, "/debts", map[string]string{"Authorization": "Bearer " + sofor}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sofor borc durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := t4JSON(t, rec)
	items, _ := body["items"].([]any)
	if len(items) != 1 || t4Sayi(body, "total_kurus") != 700 {
		t.Fatalf("borc yaniti = %v", body)
	}

	baska := t4Plaka()
	rec = o.istek(http.MethodGet, "/debts?plate="+baska, map[string]string{"Authorization": "Bearer " + sofor}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baska plaka durum = %d", rec.Code)
	}

	gorevli := o.token(auth.RoleAttendant, nil)
	rec = o.istek(http.MethodGet, "/debts?plate="+kendi, map[string]string{"Authorization": "Bearer " + gorevli}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("gorevli borc durum = %d", rec.Code)
	}
	rec = o.istek(http.MethodGet, "/debts", map[string]string{"Authorization": "Bearer " + gorevli}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("plakasiz gorevli durum = %d", rec.Code)
	}
}

func TestT4HttpExitDebtYazimi(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, false)
	bas := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Microsecond)

	// 1) Odemesiz cikis: borc tam tutar.
	plaka1 := t4Plaka()
	o.girisCikis(t, plaka1, bas, bas.Add(90*time.Minute), nil)
	if borc := o.borcToplam(t, plaka1); borc != 1000 {
		t.Fatalf("odemesiz borc = %d, beklenen 1000", borc)
	}

	// 2) Kismi odeme: borc fark kadar.
	plaka2 := t4Plaka()
	o.girisCikis(t, plaka2, bas, bas.Add(90*time.Minute), func(sessionID int64) {
		o.odeme(sessionID, 300, bas.Add(30*time.Minute))
	})
	if borc := o.borcToplam(t, plaka2); borc != 700 {
		t.Fatalf("kismi borc = %d, beklenen 700", borc)
	}

	// 3) Tam odeme: borc yok.
	plaka3 := t4Plaka()
	o.girisCikis(t, plaka3, bas, bas.Add(90*time.Minute), func(sessionID int64) {
		o.odeme(sessionID, 1000, bas.Add(30*time.Minute))
	})
	if borc := o.borcToplam(t, plaka3); borc != 0 {
		t.Fatalf("tam odemede borc = %d", borc)
	}

	// 4) Borclu plaka yeniden girer; kapi onceki borcu bildirir.
	giris := eventJSON(o.lotID, benzersiz("giris"), "entry", plaka1, bas.Add(150*time.Minute).Format(time.RFC3339Nano))
	if rec := postEvent(t, o.handler, giris); rec.Code != http.StatusCreated {
		t.Fatalf("yeniden giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	rec := o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka1), nil)
	karar := t4JSON(t, rec)
	if karar["action"] != "deny" || karar["reason"] != "prior_debt" {
		t.Fatalf("onceki borc karari = %v", karar)
	}
	if t4Sayi(karar, "prior_debt_kurus") != 1000 {
		t.Fatalf("onceki borc = %v", karar["prior_debt_kurus"])
	}
}

func (o *t4Ortam) girisCikis(t *testing.T, plaka string, giris, cikis time.Time, odemeEkle func(int64)) {
	t.Helper()
	govde := eventJSON(o.lotID, benzersiz("giris"), "entry", plaka, giris.Format(time.RFC3339Nano))
	rec := postEvent(t, o.handler, govde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("giris durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	body := t4JSON(t, rec)
	sessionID := t4Sayi(body, "session_id")
	if sessionID == 0 {
		t.Fatalf("oturum kimligi yok: %v", body)
	}
	if odemeEkle != nil {
		odemeEkle(sessionID)
	}
	cikisGovde := eventJSON(o.lotID, benzersiz("cikis"), "exit", plaka, cikis.Format(time.RFC3339Nano))
	rec = postEvent(t, o.handler, cikisGovde)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cikis durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
}

func (o *t4Ortam) borcToplam(t *testing.T, plaka string) tariff.Kurus {
	t.Helper()
	var toplam tariff.Kurus
	if err := o.pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(remaining_kurus), 0) FROM "ya-debts" WHERE plate = $1 AND status = 'open'`,
		plate.Normalize(plaka)).Scan(&toplam); err != nil {
		t.Fatalf("borc toplami: %v", err)
	}
	return toplam
}

func TestT4HttpUctanUca(t *testing.T) {
	o := t4Kur(t, 0, tariff.Band{UnitMinutes: 60, UnitPrice: 500}, true)
	entered := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Microsecond)
	plaka := t4Plaka()
	sessID := o.oturum(plaka, entered)

	rec := o.istek(http.MethodPost, "/kiosk/payments", map[string]string{
		cameraKeyHeader: testKameraAnahtari, idempotencyHeader: benzersiz("idem"),
	}, map[string]any{"session_id": sessID, "amount_kurus": 500})
	if rec.Code != http.StatusCreated {
		t.Fatalf("odeme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	odeme := t4JSON(t, rec)
	if n := t4AuditSayisi(t, o.pool, "payment.created", fmt.Sprint(t4Sayi(odeme, "payment_id"))); n != 1 {
		t.Fatalf("payment.created = %d", n)
	}

	rec = o.cihaz(http.MethodGet, fmt.Sprintf("/gate/decision?lot_id=%d&plate=%s", o.lotID, plaka), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("kapi durum = %d", rec.Code)
	}
	karar := t4JSON(t, rec)
	if karar["action"] != "open" || karar["reason"] != "paid" {
		t.Fatalf("kapi karari = %v", karar)
	}
}
