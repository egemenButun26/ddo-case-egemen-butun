package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"parkakil/internal/auth"
	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/billingsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func t5Router(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.Open(t)
	svc, err := billingsvc.New(pool)
	if err != nil {
		t.Fatalf("faturalama servisi: %v", err)
	}
	return NewRouter(Deps{
		Logger:      discardLogger(),
		Pinger:      pool,
		PingTimeout: time.Second,
		DB:          pool,
		SessionTTL:  time.Hour,
		Billing:     svc,
	}), pool
}

func t5Plan(t *testing.T, pool *pgxpool.Pool, price tariff.Kurus) string {
	t.Helper()
	lotID := testdb.NewLot(t, pool, 100)
	code := benzersiz("plan")
	if _, err := store.InsertPlan(context.Background(), pool, code, lotID, price); err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	return code
}

func t5User(t *testing.T, pool *pgxpool.Pool, role auth.Role, plate *string) store.User {
	t.Helper()
	hash, err := auth.HashPasswordCost(authTestParola, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("parola ozeti: %v", err)
	}
	email := strings.ToLower(benzersiz("t5")) + "@test.local"
	id, err := store.CreateUser(context.Background(), pool, store.CreateUserParams{
		Email:        email,
		Name:         "T5 Kullanici",
		Role:         role,
		PasswordHash: hash,
		Plate:        plate,
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

func t5Login(t *testing.T, h http.Handler, user store.User) string {
	t.Helper()
	return authLogin(t, h, user.Email, authTestParola).Token
}

func t5Request(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func t5Decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("govde cozulemedi (%q): %v", rec.Body.String(), err)
	}
}

func t5Plate(t *testing.T, onek string) string {
	t.Helper()
	raw := strings.ToUpper(strings.ReplaceAll(benzersiz(onek), "-", ""))
	if len(raw) > 8 {
		raw = raw[len(raw)-8:]
	}
	return "34" + raw
}

func t5Subscribe(t *testing.T, h http.Handler, token, plate, plan string) subscriptionView {
	t.Helper()
	body := fmt.Sprintf(`{"plate":%q,"plan_code":%q}`, plate, plan)
	rec := t5Request(t, h, http.MethodPost, "/subscriptions", body, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("abonelik olusturma durum = %d, beklenen 201; govde: %s", rec.Code, rec.Body.String())
	}
	var view subscriptionView
	t5Decode(t, rec, &view)
	return view
}

func TestT5HttpAuthRequired(t *testing.T) {
	h, _ := t5Router(t)
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/plans", ""},
		{http.MethodPost, "/subscriptions", `{"plate":"34A","plan_code":"X"}`},
		{http.MethodGet, "/subscriptions", ""},
		{http.MethodPost, "/subscriptions/1/cancel", ""},
		{http.MethodPost, "/subscriptions/1/change-plan", `{"plan_code":"X"}`},
		{http.MethodGet, "/invoices", ""},
		{http.MethodPost, "/billing/period-end", `{"period_start":"2030-01-01"}`},
		{http.MethodPost, "/invoices/1/payment-attempts", `{"success":false}`},
		{http.MethodPost, "/reconciliation/2030-01-01/provider-report", `[]`},
		{http.MethodGet, "/reconciliation/2030-01-01", ""},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := t5Request(t, h, c.method, c.path, c.body, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
			}
			assertErrorBody(t, rec, "unauthorized")
		})
	}
}

func TestT5HttpRoleForbidden(t *testing.T) {
	h, pool := t5Router(t)
	driver := t5User(t, pool, auth.RoleDriver, nil)
	attendant := t5User(t, pool, auth.RoleAttendant, nil)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	manager := t5User(t, pool, auth.RoleManager, nil)

	driverToken := t5Login(t, h, driver)
	attendantToken := t5Login(t, h, attendant)
	accountantToken := t5Login(t, h, accountant)
	managerToken := t5Login(t, h, manager)

	cases := []struct {
		name, method, path, body, token string
	}{
		{"sofor donem sonu", http.MethodPost, "/billing/period-end", `{"period_start":"2030-02-01"}`, driverToken},
		{"muhasebe abonelik", http.MethodPost, "/subscriptions", `{"plate":"34A","plan_code":"X"}`, accountantToken},
		{"gorevli abonelik listesi", http.MethodGet, "/subscriptions", "", attendantToken},
		{"sofor mutabakat", http.MethodPost, "/reconciliation/2030-01-01/provider-report", `[]`, driverToken},
		{"sofor odeme denemesi", http.MethodPost, "/invoices/1/payment-attempts", `{"success":false}`, driverToken},
		{"muhasebe iptal", http.MethodPost, "/subscriptions/1/cancel", "", accountantToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := t5Request(t, h, c.method, c.path, c.body, c.token)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("durum = %d, beklenen 403; govde: %s", rec.Code, rec.Body.String())
			}
			assertErrorBody(t, rec, "forbidden")
		})
	}

	if rec := t5Request(t, h, http.MethodGet, "/plans", "", attendantToken); rec.Code != http.StatusOK {
		t.Fatalf("gorevli planlar durum = %d, beklenen 200", rec.Code)
	}
	if rec := t5Request(t, h, http.MethodGet, "/invoices", "", managerToken); rec.Code != http.StatusOK {
		t.Fatalf("mudur fatura listesi durum = %d, beklenen 200", rec.Code)
	}
}

func TestT5HttpPlans(t *testing.T) {
	h, pool := t5Router(t)
	driver := t5User(t, pool, auth.RoleDriver, nil)
	token := t5Login(t, h, driver)
	code := t5Plan(t, pool, 150000)

	rec := t5Request(t, h, http.MethodGet, "/plans", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var body plansResponse
	t5Decode(t, rec, &body)
	bulundu := false
	for _, p := range body.Items {
		if p.Code == code {
			bulundu = true
			if p.Monthly != 150000 || !p.Active {
				t.Fatalf("plan alanlari yanlis: %+v", p)
			}
		}
	}
	if !bulundu {
		t.Fatalf("plan listede yok: %s", code)
	}
}

func TestT5HttpSubscribe(t *testing.T) {
	h, pool := t5Router(t)
	plate := t5Plate(t, "sofor")
	otherPlate := t5Plate(t, "baska")
	driver := t5User(t, pool, auth.RoleDriver, &plate)
	manager := t5User(t, pool, auth.RoleManager, nil)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	driverToken := t5Login(t, h, driver)
	managerToken := t5Login(t, h, manager)
	accountantToken := t5Login(t, h, accountant)
	plan := t5Plan(t, pool, 150000)

	sub := t5Subscribe(t, h, driverToken, plate, plan)
	if sub.Plate != plate || sub.PlanCode != plan || sub.Status != string(billing.StatusActive) {
		t.Fatalf("abonelik alanlari yanlis: %+v", sub)
	}
	if sub.UserID == nil || *sub.UserID != driver.ID {
		t.Fatalf("sofor aboneligi sahibi yanlis: %v", sub.UserID)
	}
	if sub.CurrentPeriodStart.Day() != 1 {
		t.Fatalf("donem baslangici ayin 1'i degil: %v", sub.CurrentPeriodStart)
	}

	rec := t5Request(t, h, http.MethodPost, "/subscriptions",
		fmt.Sprintf(`{"plate":%q,"plan_code":%q}`, otherPlate, plan), driverToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baska plaka durum = %d, beklenen 403; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "forbidden")

	managerSub := t5Subscribe(t, h, managerToken, otherPlate, plan)
	if managerSub.Plate != otherPlate {
		t.Fatalf("mudur aboneligi yanlis: %+v", managerSub)
	}

	rec = t5Request(t, h, http.MethodPost, "/subscriptions",
		fmt.Sprintf(`{"plate":%q,"plan_code":"yok-plan"}`, plate), driverToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan plan durum = %d, beklenen 404; govde: %s", rec.Code, rec.Body.String())
	}

	rec = t5Request(t, h, http.MethodPost, "/subscriptions",
		fmt.Sprintf(`{"plate":%q,"plan_code":%q}`, plate, plan), driverToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cift abonelik durum = %d, beklenen 409; govde: %s", rec.Code, rec.Body.String())
	}

	rec = t5Request(t, h, http.MethodPost, "/subscriptions",
		fmt.Sprintf(`{"plate":%q,"plan_code":%q}`, plate, plan), accountantToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("muhasebe abonelik durum = %d, beklenen 403", rec.Code)
	}

	if n := auditSayisi(t, pool, "billing.subscription_created"); n < 2 {
		t.Fatalf("abonelik denetim kaydi = %d, en az 2 beklenirdi", n)
	}
}

func TestT5HttpSubscriptionsList(t *testing.T) {
	h, pool := t5Router(t)
	plate := t5Plate(t, "liste")
	otherPlate := t5Plate(t, "diger")
	driver := t5User(t, pool, auth.RoleDriver, &plate)
	other := t5User(t, pool, auth.RoleDriver, &otherPlate)
	manager := t5User(t, pool, auth.RoleManager, nil)
	driverToken := t5Login(t, h, driver)
	otherToken := t5Login(t, h, other)
	managerToken := t5Login(t, h, manager)

	var ids []int64
	for i := 0; i < 3; i++ {
		plan := t5Plan(t, pool, tariff.Kurus(1000+i))
		ids = append(ids, t5Subscribe(t, h, driverToken, plate, plan).ID)
	}
	otherSub := t5Subscribe(t, h, otherToken, otherPlate, t5Plan(t, pool, 2000))

	first := t5Request(t, h, http.MethodGet, "/subscriptions?limit=2", "", driverToken)
	if first.Code != http.StatusOK {
		t.Fatalf("ilk sayfa durum = %d; govde: %s", first.Code, first.Body.String())
	}
	var page1 subscriptionsResponse
	t5Decode(t, first, &page1)
	if len(page1.Items) != 2 || page1.NextAfterID == nil {
		t.Fatalf("ilk sayfa yanlis: %+v", page1)
	}
	for _, item := range page1.Items {
		if item.Plate != plate {
			t.Fatalf("sofor baska plaka gordu: %+v", item)
		}
	}
	ikinci := t5Request(t, h, http.MethodGet,
		fmt.Sprintf("/subscriptions?after_id=%d&limit=10", *page1.NextAfterID), "", driverToken)
	if ikinci.Code != http.StatusOK {
		t.Fatalf("ikinci sayfa durum = %d", ikinci.Code)
	}
	var page2 subscriptionsResponse
	t5Decode(t, ikinci, &page2)
	if len(page2.Items) == 0 {
		t.Fatal("ikinci sayfa bos")
	}
	for _, item := range page2.Items {
		if item.ID <= *page1.NextAfterID {
			t.Fatalf("keyset ihlali: %d <= %d", item.ID, *page1.NextAfterID)
		}
		if item.Plate != plate {
			t.Fatalf("sofor baska plaka gordu: %+v", item)
		}
	}

	rec := t5Request(t, h, http.MethodGet, "/subscriptions?plate="+otherPlate, "", driverToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baska plaka sorgusu durum = %d, beklenen 403", rec.Code)
	}

	managerRec := t5Request(t, h, http.MethodGet, "/subscriptions?limit=100", "", managerToken)
	if managerRec.Code != http.StatusOK {
		t.Fatalf("mudur listesi durum = %d", managerRec.Code)
	}
	var managerBody subscriptionsResponse
	t5Decode(t, managerRec, &managerBody)
	foundOther, foundDriver := false, false
	for _, item := range managerBody.Items {
		if item.ID == otherSub.ID {
			foundOther = true
		}
		for _, id := range ids {
			if item.ID == id {
				foundDriver = true
			}
		}
	}
	if !foundDriver || !foundOther {
		t.Fatalf("mudur tum abonelikleri gormedi: driver=%v other=%v", foundDriver, foundOther)
	}

	for _, sorgu := range []string{"after_id=abc", "limit=0", "limit=101"} {
		rec := t5Request(t, h, http.MethodGet, "/subscriptions?"+sorgu, "", driverToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s durum = %d, beklenen 400", sorgu, rec.Code)
		}
		assertErrorBody(t, rec, "invalid_pagination")
	}
}

func TestT5HttpCancel(t *testing.T) {
	h, pool := t5Router(t)
	plate := t5Plate(t, "iptal")
	otherPlate := t5Plate(t, "yabanci")
	driver := t5User(t, pool, auth.RoleDriver, &plate)
	other := t5User(t, pool, auth.RoleDriver, &otherPlate)
	manager := t5User(t, pool, auth.RoleManager, nil)
	driverToken := t5Login(t, h, driver)
	otherToken := t5Login(t, h, other)
	managerToken := t5Login(t, h, manager)
	plan := t5Plan(t, pool, 300000)

	sub := t5Subscribe(t, h, driverToken, plate, plan)
	rec := t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/cancel", sub.ID), "", otherToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("yabanci iptal durum = %d, beklenen 403; govde: %s", rec.Code, rec.Body.String())
	}

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/cancel", sub.ID), "", driverToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("iptal durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var body cancelSubscriptionResponse
	t5Decode(t, rec, &body)
	if body.SubscriptionID != sub.ID || body.Status != string(billing.StatusCancelled) {
		t.Fatalf("iptal yaniti yanlis: %+v", body)
	}
	if body.Credit <= 0 || body.InvoiceID == 0 {
		t.Fatalf("iptal kredisi/faturasi yok: %+v", body)
	}
	inv, err := store.GetInvoice(context.Background(), pool, body.InvoiceID)
	if err != nil {
		t.Fatalf("kredi faturasi okunamadi: %v", err)
	}
	if inv.Amount != tariff.Kurus(body.Credit) || inv.Kind != store.InvoiceKindProrationCredit {
		t.Fatalf("kredi faturasi yanlis: %+v", inv)
	}
	guncel, err := store.GetSubscription(context.Background(), pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.Status != billing.StatusCancelled || guncel.CancelledAt == nil {
		t.Fatalf("abonelik iptal durumu yanlis: %+v", guncel)
	}

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/cancel", sub.ID), "", driverToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("ikinci iptal durum = %d, beklenen 409", rec.Code)
	}

	managerSub := t5Subscribe(t, h, managerToken, t5Plate(t, "mudur"), plan)
	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/cancel", managerSub.ID), "", managerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("mudur iptal durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	if n := auditSayisi(t, pool, "billing.subscription_cancelled"); n < 2 {
		t.Fatalf("iptal denetim kaydi = %d, en az 2 beklenirdi", n)
	}
}

func TestT5HttpChangePlan(t *testing.T) {
	h, pool := t5Router(t)
	plate := t5Plate(t, "gecis")
	otherPlate := t5Plate(t, "yabanci")
	driver := t5User(t, pool, auth.RoleDriver, &plate)
	other := t5User(t, pool, auth.RoleDriver, &otherPlate)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	driverToken := t5Login(t, h, driver)
	otherToken := t5Login(t, h, other)
	accountantToken := t5Login(t, h, accountant)
	ucuz := t5Plan(t, pool, 300000)
	pahali := t5Plan(t, pool, 600000)
	orta := t5Plan(t, pool, 400000)

	sub := t5Subscribe(t, h, driverToken, plate, ucuz)
	rec := t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		fmt.Sprintf(`{"plan_code":%q}`, pahali), otherToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("yabanci gecis durum = %d, beklenen 403", rec.Code)
	}

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		fmt.Sprintf(`{"plan_code":%q}`, pahali), driverToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan gecisi durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var first changePlanResponse
	t5Decode(t, rec, &first)
	if first.NewPlanCode != pahali || first.Charge <= 0 || first.Credit <= 0 || first.Net <= 0 ||
		first.ChargeInvoiceID == 0 || first.CreditInvoiceID == 0 {
		t.Fatalf("plan gecisi yaniti yanlis: %+v", first)
	}
	chargeID, creditID := first.ChargeInvoiceID, first.CreditInvoiceID

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		fmt.Sprintf(`{"plan_code":%q}`, orta), driverToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("ikinci plan gecisi durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var second changePlanResponse
	t5Decode(t, rec, &second)
	if second.ChargeInvoiceID != chargeID || second.CreditInvoiceID != creditID {
		t.Fatalf("ayni donem proration faturasi birlestirilmedi: %+v", second)
	}

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		fmt.Sprintf(`{"plan_code":%q}`, orta), driverToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("ayni plan durum = %d, beklenen 409", rec.Code)
	}
	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		`{"plan_code":"yok-plan"}`, driverToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan plan durum = %d, beklenen 404", rec.Code)
	}

	once, err := store.GetInvoice(context.Background(), pool, chargeID)
	if err != nil {
		t.Fatalf("ucret faturasi okunamadi: %v", err)
	}
	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/invoices/%d/payment-attempts", chargeID),
		`{"success":true}`, accountantToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("gecis faturasi odeme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID),
		fmt.Sprintf(`{"plan_code":%q}`, pahali), driverToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("odendi proration gecisi durum = %d, beklenen 409; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "proration_invoice_closed")
	sonra, err := store.GetInvoice(context.Background(), pool, chargeID)
	if err != nil {
		t.Fatalf("ucret faturasi okunamadi: %v", err)
	}
	if sonra.Amount != once.Amount || sonra.Status != store.InvoiceStatusPaid {
		t.Fatalf("odendi fatura degisti: once=%+v sonra=%+v", once, sonra)
	}

	guncel, err := store.GetSubscription(context.Background(), pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.PlanCode != orta {
		t.Fatalf("plan = %s, beklenen %s", guncel.PlanCode, orta)
	}
	if n := auditSayisi(t, pool, "billing.plan_changed"); n < 2 {
		t.Fatalf("plan gecisi denetim kaydi = %d, en az 2 beklenirdi", n)
	}
}

func TestT5HttpInvoicesList(t *testing.T) {
	h, pool := t5Router(t)
	plate := t5Plate(t, "fatura")
	otherPlate := t5Plate(t, "yabanci")
	driver := t5User(t, pool, auth.RoleDriver, &plate)
	other := t5User(t, pool, auth.RoleDriver, &otherPlate)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	driverToken := t5Login(t, h, driver)
	otherToken := t5Login(t, h, other)
	accountantToken := t5Login(t, h, accountant)
	plan := t5Plan(t, pool, 100000)

	sub := t5Subscribe(t, h, driverToken, plate, plan)
	ps := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	pe := ps.AddDate(0, 1, 0)
	base := store.Invoice{
		SubscriptionID: sub.ID,
		PeriodStart:    ps,
		PeriodEnd:      pe,
		Kind:           store.InvoiceKindPeriod,
		Amount:         100000,
		Status:         store.InvoiceStatusOpen,
	}
	first, err := store.InsertInvoice(context.Background(), pool, base)
	if err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}
	base.Kind = store.InvoiceKindProrationCharge
	base.Status = store.InvoiceStatusPaid
	paidAt := time.Date(2035, 1, 5, 12, 0, 0, 0, time.UTC)
	base.PaidAt = &paidAt
	second, err := store.InsertInvoice(context.Background(), pool, base)
	if err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}
	base.Kind = store.InvoiceKindProrationCredit
	base.Status = store.InvoiceStatusOpen
	base.PaidAt = nil
	if _, err := store.InsertInvoice(context.Background(), pool, base); err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}

	rec := t5Request(t, h, http.MethodGet, fmt.Sprintf("/invoices?subscription_id=%d", sub.ID), "", driverToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("sofor fatura listesi durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var body invoicesResponse
	t5Decode(t, rec, &body)
	if len(body.Items) != 3 {
		t.Fatalf("fatura sayisi = %d, beklenen 3", len(body.Items))
	}

	rec = t5Request(t, h, http.MethodGet, fmt.Sprintf("/invoices?subscription_id=%d", sub.ID), "", otherToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("yabanci fatura durum = %d, beklenen 403", rec.Code)
	}

	rec = t5Request(t, h, http.MethodGet, "/invoices?status=paid", "", accountantToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("muhasebe filtre durum = %d", rec.Code)
	}
	var paidBody invoicesResponse
	t5Decode(t, rec, &paidBody)
	if len(paidBody.Items) == 0 {
		t.Fatal("odendi filtresi bos")
	}
	for _, item := range paidBody.Items {
		if item.Status != store.InvoiceStatusPaid {
			t.Fatalf("filtre disi fatura: %+v", item)
		}
	}

	ilk := t5Request(t, h, http.MethodGet, fmt.Sprintf("/invoices?subscription_id=%d&limit=1", sub.ID), "", driverToken)
	var page1 invoicesResponse
	t5Decode(t, ilk, &page1)
	if len(page1.Items) != 1 || page1.NextAfterID == nil {
		t.Fatalf("ilk fatura sayfasi yanlis: %+v", page1)
	}
	if page1.Items[0].ID != first {
		t.Fatalf("ilk fatura = %d, beklenen %d", page1.Items[0].ID, first)
	}
	ikinci := t5Request(t, h, http.MethodGet,
		fmt.Sprintf("/invoices?subscription_id=%d&after_id=%d&limit=1", sub.ID, *page1.NextAfterID), "", driverToken)
	var page2 invoicesResponse
	t5Decode(t, ikinci, &page2)
	if len(page2.Items) != 1 || page2.Items[0].ID != second {
		t.Fatalf("ikinci fatura sayfasi yanlis: %+v", page2)
	}

	rec = t5Request(t, h, http.MethodGet, "/invoices?status=gecersiz", "", driverToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("gecersiz durum filtresi = %d, beklenen 400", rec.Code)
	}
}

func TestT5HttpPeriodEnd(t *testing.T) {
	h, pool := t5Router(t)
	admin := t5User(t, pool, auth.RoleAdmin, nil)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	driver := t5User(t, pool, auth.RoleDriver, nil)
	adminToken := t5Login(t, h, admin)
	accountantToken := t5Login(t, h, accountant)
	driverToken := t5Login(t, h, driver)

	plate := t5Plate(t, "donem")
	owner := t5User(t, pool, auth.RoleDriver, &plate)
	ownerToken := t5Login(t, h, owner)
	plan := t5Plan(t, pool, 50000)
	sub := t5Subscribe(t, h, ownerToken, plate, plan)

	rec := t5Request(t, h, http.MethodPost, "/billing/period-end", `{"period_start":"2031-02-01"}`, driverToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor donem sonu durum = %d, beklenen 403", rec.Code)
	}
	rec = t5Request(t, h, http.MethodPost, "/billing/period-end", `{"period_start":"2031-02-01"}`, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("donem sonu durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var body periodEndResponse
	t5Decode(t, rec, &body)
	if body.InvoicesCreated < 1 {
		t.Fatalf("olusturulan fatura = %d, en az 1 beklenirdi", body.InvoicesCreated)
	}

	rec = t5Request(t, h, http.MethodPost, "/billing/period-end", `{"period_start":"2031-02-01"}`, accountantToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("ikinci donem sonu durum = %d", rec.Code)
	}
	var second periodEndResponse
	t5Decode(t, rec, &second)
	if second.InvoicesCreated != 0 {
		t.Fatalf("ikinci donem sonu %d fatura uretti, beklenen 0", second.InvoicesCreated)
	}

	var periodInvoice int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-invoices" WHERE subscription_id = $1 AND kind = 'period' AND period_start = '2031-02-01'`,
		sub.ID,
	).Scan(&periodInvoice); err != nil {
		t.Fatalf("fatura sayisi: %v", err)
	}
	if periodInvoice != 1 {
		t.Fatalf("donem faturasi = %d, beklenen 1", periodInvoice)
	}
	guncel, err := store.GetSubscription(context.Background(), pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.CurrentPeriodStart.Month() != time.March || guncel.CurrentPeriodStart.Year() != 2031 {
		t.Fatalf("donem ilerlemedi: %v", guncel.CurrentPeriodStart)
	}
	if n := auditSayisi(t, pool, "billing.period_end"); n < 2 {
		t.Fatalf("donem sonu denetim kaydi = %d, en az 2 beklenirdi", n)
	}

	for _, govde := range []string{`{}`, `{"period_start":"2031-02-15"}`, `{"period_start":"abc"}`, `{"period_start":"2031-2-1"}`} {
		rec := t5Request(t, h, http.MethodPost, "/billing/period-end", govde, adminToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s durum = %d, beklenen 400", govde, rec.Code)
		}
	}
}

func TestT5HttpPaymentAttempts(t *testing.T) {
	h, pool := t5Router(t)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	admin := t5User(t, pool, auth.RoleAdmin, nil)
	driver := t5User(t, pool, auth.RoleDriver, nil)
	accountantToken := t5Login(t, h, accountant)
	adminToken := t5Login(t, h, admin)
	driverToken := t5Login(t, h, driver)

	plate := t5Plate(t, "dunning")
	owner := t5User(t, pool, auth.RoleDriver, &plate)
	ownerToken := t5Login(t, h, owner)
	sub := t5Subscribe(t, h, ownerToken, plate, t5Plan(t, pool, 100000))

	ps := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	pe := ps.AddDate(0, 1, 0)
	invID, err := store.InsertInvoice(context.Background(), pool, store.Invoice{
		SubscriptionID: sub.ID,
		PeriodStart:    ps,
		PeriodEnd:      pe,
		Kind:           store.InvoiceKindPeriod,
		Amount:         100000,
		Status:         store.InvoiceStatusOpen,
	})
	if err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}

	rec := t5Request(t, h, http.MethodPost, fmt.Sprintf("/invoices/%d/payment-attempts", invID), `{"success":false}`, driverToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor odeme denemesi durum = %d, beklenen 403", rec.Code)
	}

	var last paymentAttemptResponse
	for i := 0; i < 3; i++ {
		token := accountantToken
		if i == 2 {
			token = adminToken
		}
		rec = t5Request(t, h, http.MethodPost,
			fmt.Sprintf("/invoices/%d/payment-attempts", invID), `{"success":false}`, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d. basarisiz deneme durum = %d; govde: %s", i+1, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &last); err != nil {
			t.Fatalf("yanit cozulemedi: %v", err)
		}
		if last.Attempts != i+1 || last.Status != store.InvoiceStatusFailed || last.NextRetryAt == nil {
			t.Fatalf("%d. deneme yaniti yanlis: %+v", i+1, last)
		}
	}
	if last.SubscriptionStatus != string(billing.StatusSuspended) {
		t.Fatalf("ucuncu basarisizlik sonrasi abonelik = %q, beklenen suspended", last.SubscriptionStatus)
	}

	plate2 := t5Plate(t, "odeme")
	owner2 := t5User(t, pool, auth.RoleDriver, &plate2)
	owner2Token := t5Login(t, h, owner2)
	sub2 := t5Subscribe(t, h, owner2Token, plate2, t5Plan(t, pool, 100000))
	payID, err := store.InsertInvoice(context.Background(), pool, store.Invoice{
		SubscriptionID: sub2.ID,
		PeriodStart:    ps,
		PeriodEnd:      pe,
		Kind:           store.InvoiceKindPeriod,
		Amount:         100000,
		Status:         store.InvoiceStatusOpen,
	})
	if err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}
	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/invoices/%d/payment-attempts", payID), `{"success":true}`, accountantToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("basarili odeme durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var odendi paymentAttemptResponse
	t5Decode(t, rec, &odendi)
	if odendi.Status != store.InvoiceStatusPaid || odendi.Attempts != 0 || odendi.SubscriptionStatus != string(billing.StatusActive) {
		t.Fatalf("basarili odeme yaniti yanlis: %+v", odendi)
	}

	rec = t5Request(t, h, http.MethodPost, fmt.Sprintf("/invoices/%d/payment-attempts", invID), `{}`, accountantToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("success eksik durum = %d, beklenen 400", rec.Code)
	}
	rec = t5Request(t, h, http.MethodPost, "/invoices/999999999/payment-attempts", `{"success":false}`, accountantToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan fatura durum = %d, beklenen 404", rec.Code)
	}
	if n := auditSayisi(t, pool, "billing.payment_attempt"); n < 4 {
		t.Fatalf("odeme denemesi denetim kaydi = %d, en az 4 beklenirdi", n)
	}
}

func TestT5HttpReconciliation(t *testing.T) {
	h, pool := t5Router(t)
	accountant := t5User(t, pool, auth.RoleAccountant, nil)
	manager := t5User(t, pool, auth.RoleManager, nil)
	admin := t5User(t, pool, auth.RoleAdmin, nil)
	driver := t5User(t, pool, auth.RoleDriver, nil)
	accountantToken := t5Login(t, h, accountant)
	managerToken := t5Login(t, h, manager)
	adminToken := t5Login(t, h, admin)
	driverToken := t5Login(t, h, driver)

	plate := t5Plate(t, "mutabakat")
	owner := t5User(t, pool, auth.RoleDriver, &plate)
	ownerToken := t5Login(t, h, owner)
	sub := t5Subscribe(t, h, ownerToken, plate, t5Plan(t, pool, 100000))

	ps := time.Date(2033, 5, 1, 0, 0, 0, 0, time.UTC)
	pe := ps.AddDate(0, 1, 0)
	paidAt := time.Date(2033, 5, 10, 12, 0, 0, 0, time.UTC)
	makeInvoice := func(kind string, amount tariff.Kurus) int64 {
		t.Helper()
		id, err := store.InsertInvoice(context.Background(), pool, store.Invoice{
			SubscriptionID: sub.ID,
			PeriodStart:    ps,
			PeriodEnd:      pe,
			Kind:           kind,
			Amount:         amount,
			Status:         store.InvoiceStatusPaid,
			PaidAt:         &paidAt,
		})
		if err != nil {
			t.Fatalf("odendi fatura eklenemedi: %v", err)
		}
		return id
	}
	inv1 := makeInvoice(store.InvoiceKindPeriod, 500)
	inv2 := makeInvoice(store.InvoiceKindProrationCharge, 300)
	inv3 := makeInvoice(store.InvoiceKindProrationCredit, 200)

	body := fmt.Sprintf(`[
		{"provider_ref":"invoice:%d","amount_kurus":500},
		{"provider_ref":"invoice:%d","amount_kurus":250},
		{"provider_ref":"fazla-ref","amount_kurus":700}
	]`, inv1, inv2)
	rec := t5Request(t, h, http.MethodPost, "/reconciliation/2033-05-10/provider-report", body, accountantToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("mutabakat ice aktarma durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var first reconciliationImportResponse
	t5Decode(t, rec, &first)
	if first.Imported != 3 {
		t.Fatalf("ice aktarilan = %d, beklenen 3", first.Imported)
	}
	if len(first.Report.Matched) != 1 || len(first.Report.MissingInProvider) != 1 ||
		len(first.Report.MissingInOurs) != 1 || len(first.Report.AmountMismatch) != 1 {
		t.Fatalf("rapor listeleri yanlis: %+v", first.Report)
	}
	if first.Report.MissingInProvider[0].ProviderRef != fmt.Sprintf("invoice:%d", inv3) {
		t.Fatalf("saglayicida eksik yanlis: %+v", first.Report.MissingInProvider)
	}
	if first.Report.MissingInOurs[0].ProviderRef != "fazla-ref" {
		t.Fatalf("bizde eksik yanlis: %+v", first.Report.MissingInOurs)
	}
	if m := first.Report.AmountMismatch[0]; m.Ours != 300 || m.Provider != 250 {
		t.Fatalf("tutar farki yanlis: %+v", m)
	}
	if first.Report.OurTotal != 1000 || first.Report.ProviderTotal != 1450 || first.Report.Diff != 450 {
		t.Fatalf("toplamlar yanlis: %+v", first.Report)
	}

	rec = t5Request(t, h, http.MethodPost, "/reconciliation/2033-05-10/provider-report", body, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("ikinci ice aktarma durum = %d", rec.Code)
	}
	var second reconciliationImportResponse
	t5Decode(t, rec, &second)
	if second.Report.OurTotal != first.Report.OurTotal ||
		second.Report.ProviderTotal != first.Report.ProviderTotal ||
		second.Report.Diff != first.Report.Diff {
		t.Fatalf("ikinci rapor farkli: %+v", second.Report)
	}
	var settlementCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "ya-provider_settlements" WHERE day = '2033-05-10'`,
	).Scan(&settlementCount); err != nil {
		t.Fatalf("mutabakat satiri sayisi: %v", err)
	}
	if settlementCount != 3 {
		t.Fatalf("saglayici satiri = %d, beklenen 3", settlementCount)
	}

	rec = t5Request(t, h, http.MethodGet, "/reconciliation/2033-05-10", "", managerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("rapor okuma durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var got billing.Report
	t5Decode(t, rec, &got)
	if len(got.Matched) != 1 || got.OurTotal != 1000 || got.ProviderTotal != 1450 || got.Diff != 450 {
		t.Fatalf("okunan rapor yanlis: %+v", got)
	}
	rec = t5Request(t, h, http.MethodGet, "/reconciliation/2033-05-10", "", driverToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sofor rapor durum = %d, beklenen 403", rec.Code)
	}
	rec = t5Request(t, h, http.MethodGet, "/reconciliation/2034-01-01", "", accountantToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("olmayan rapor durum = %d, beklenen 404", rec.Code)
	}
	rec = t5Request(t, h, http.MethodPost, "/reconciliation/gecersiz/provider-report", `[]`, accountantToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("gecersiz gun durum = %d, beklenen 400", rec.Code)
	}
	rec = t5Request(t, h, http.MethodPost, "/reconciliation/2033-05-11/provider-report",
		`[{"provider_ref":"","amount_kurus":10}]`, accountantToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bos ref durum = %d, beklenen 400", rec.Code)
	}

	if n := auditSayisi(t, pool, "billing.reconciliation"); n < 2 {
		t.Fatalf("mutabakat denetim kaydi = %d, en az 2 beklenirdi", n)
	}
}

func TestT5HttpBadRequests(t *testing.T) {
	h, pool := t5Router(t)
	admin := t5User(t, pool, auth.RoleAdmin, nil)
	manager := t5User(t, pool, auth.RoleManager, nil)
	adminToken := t5Login(t, h, admin)
	managerToken := t5Login(t, h, manager)
	plate := t5Plate(t, "kotu")
	plateOwner := t5User(t, pool, auth.RoleDriver, &plate)
	ownerToken := t5Login(t, h, plateOwner)
	plan := t5Plan(t, pool, 1000)
	sub := t5Subscribe(t, h, ownerToken, plate, plan)

	cases := []struct {
		name, method, path, body, token, code string
	}{
		{"bozuk json abonelik", http.MethodPost, "/subscriptions", `{"plate":`, ownerToken, "invalid_json"},
		{"fazla alan abonelik", http.MethodPost, "/subscriptions", `{"plate":"X","plan_code":"Y","rol":"admin"}`, ownerToken, "invalid_json"},
		{"bos plaka", http.MethodPost, "/subscriptions", `{"plate":" ","plan_code":"X"}`, ownerToken, "invalid_request"},
		{"bozuk id iptal", http.MethodPost, "/subscriptions/abc/cancel", "", ownerToken, "invalid_id"},
		{"bozuk id gecis", http.MethodPost, "/subscriptions/0/change-plan", `{"plan_code":"X"}`, ownerToken, "invalid_id"},
		{"bos plan kodu", http.MethodPost, fmt.Sprintf("/subscriptions/%d/change-plan", sub.ID), `{"plan_code":" "}`, ownerToken, "invalid_request"},
		{"bozuk id odeme", http.MethodPost, "/invoices/abc/payment-attempts", `{"success":false}`, adminToken, "invalid_id"},
		{"gecersiz abonelik id", http.MethodGet, "/invoices?subscription_id=abc", "", ownerToken, "invalid_pagination"},
		{"gecersiz fatura id", http.MethodPost, "/invoices/-5/payment-attempts", `{"success":false}`, adminToken, "invalid_id"},
		{"bos mutabakat govdesi", http.MethodPost, "/reconciliation/2033-06-01/provider-report", ``, adminToken, "invalid_json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := t5Request(t, h, c.method, c.path, c.body, c.token)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("durum = %d, beklenen 400; govde: %s", rec.Code, rec.Body.String())
			}
			assertErrorBody(t, rec, c.code)
		})
	}

	rec := t5Request(t, h, http.MethodGet, "/subscriptions?limit=abc", "", managerToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit durum = %d, beklenen 400", rec.Code)
	}
}
