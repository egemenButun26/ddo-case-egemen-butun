package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"parkakil/internal/auth"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

const authTestParola = "test-parola-123"

func authRouter(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.Open(t)
	return NewRouter(Deps{
		Logger:      discardLogger(),
		Pinger:      pool,
		PingTimeout: time.Second,
		DB:          pool,
		SessionTTL:  time.Hour,
	}), pool
}

func authKullanici(t *testing.T, pool *pgxpool.Pool, role auth.Role, active bool) store.User {
	t.Helper()
	hash, err := auth.HashPasswordCost(authTestParola, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("parola ozeti: %v", err)
	}
	email := strings.ToLower(benzersiz("auth")) + "@test.local"
	id, err := store.CreateUser(context.Background(), pool, store.CreateUserParams{
		Email:        email,
		Name:         "Auth Kullanici",
		Role:         role,
		PasswordHash: hash,
		Active:       &active,
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

func authBody(t *testing.T, email, password string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatalf("govde: %v", err)
	}
	return b
}

func authPost(t *testing.T, h http.Handler, path string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func authGet(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func authLogin(t *testing.T, h http.Handler, email, password string) loginResponse {
	t.Helper()
	rec := authPost(t, h, "/auth/login", authBody(t, email, password), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("giris durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("giris govdesi cozulemedi: %v", err)
	}
	return body
}

func auditSayisi(t *testing.T, pool *pgxpool.Pool, action string) int {
	t.Helper()
	var sayi int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1`, action,
	).Scan(&sayi); err != nil {
		t.Fatalf("denetim sayisi okunamadi: %v", err)
	}
	return sayi
}

func TestAuthLoginSuccess(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)

	body := authLogin(t, h, user.Email, authTestParola)
	if body.Token == "" {
		t.Fatal("token bos")
	}
	if !body.ExpiresAt.After(time.Now()) {
		t.Fatalf("expires_at gelecekte degil: %v", body.ExpiresAt)
	}
	if body.User.ID != user.ID || body.User.Email != user.Email || body.User.Name != user.Name || body.User.Role != auth.RoleDriver {
		t.Fatalf("kullanici alanlari yanlis: %+v", body.User)
	}
	if strings.Contains(authPost(t, h, "/auth/login", authBody(t, user.Email, authTestParola), "").Body.String(), "password") {
		t.Fatal("giris yanitinda parola alani var")
	}
	if _, err := store.GetAuthSessionByTokenHash(context.Background(), pool, auth.HashToken(body.Token)); err != nil {
		t.Fatalf("oturum kaydedilmemis: %v", err)
	}
	if auditSayisi(t, pool, "auth.login") < 1 {
		t.Fatal("auth.login denetim kaydi yok")
	}

	me := authGet(t, h, "/auth/me", body.Token)
	if me.Code != http.StatusOK {
		t.Fatalf("/auth/me durum = %d; govde: %s", me.Code, me.Body.String())
	}
	if strings.Contains(me.Body.String(), "password") {
		t.Fatal("/auth/me yanitinda parola alani var")
	}
}

func TestAuthLoginWrongPassword(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	parola := "yanlis-parola-gizli"

	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, parola), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "invalid_credentials")
	if strings.Contains(rec.Body.String(), "parola") {
		t.Fatal("hata govdesi parola sizdiriyor")
	}
	if auditSayisi(t, pool, "auth.login_failed") < 1 {
		t.Fatal("auth.login_failed denetim kaydi yok")
	}
	var yeni string
	if err := pool.QueryRow(context.Background(),
		`SELECT coalesce(new_value::text, '') FROM audit_log WHERE action = 'auth.login_failed' ORDER BY id DESC LIMIT 1`,
	).Scan(&yeni); err != nil {
		t.Fatalf("denetim kaydi okunamadi: %v", err)
	}
	if strings.Contains(yeni, parola) {
		t.Fatalf("denetim kaydi parola iceriyor: %s", yeni)
	}
}

func TestAuthLoginUnknownEmail(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)

	yanlisParola := authPost(t, h, "/auth/login", authBody(t, user.Email, "yanlis"), "")
	bilinmeyen := authPost(t, h, "/auth/login", authBody(t, "yok@test.local", "yanlis"), "")

	if yanlisParola.Code != http.StatusUnauthorized || bilinmeyen.Code != http.StatusUnauthorized {
		t.Fatalf("durumlar = %d/%d, beklenen 401", yanlisParola.Code, bilinmeyen.Code)
	}
	if yanlisParola.Body.String() != bilinmeyen.Body.String() {
		t.Fatalf("yanitlar farkli: %q != %q", yanlisParola.Body.String(), bilinmeyen.Body.String())
	}
	assertErrorBody(t, bilinmeyen, "invalid_credentials")
}

func TestAuthLoginInactive(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, false)

	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, authTestParola), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "invalid_credentials")
	if auditSayisi(t, pool, "auth.login_failed") < 1 {
		t.Fatal("pasif kullanici icin denetim kaydi yok")
	}
}

func TestAuthLoginMalformed(t *testing.T) {
	h, _ := authRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bozuk json durum = %d, beklenen 400", rec.Code)
	}
	assertErrorBody(t, rec, "invalid_json")

	fazla := authPost(t, h, "/auth/login", []byte(`{"email":"a@b.c","password":"x","rol":"admin"}`), "")
	if fazla.Code != http.StatusBadRequest {
		t.Fatalf("fazla alan durum = %d, beklenen 400", fazla.Code)
	}
	assertErrorBody(t, fazla, "invalid_json")

	bos := authPost(t, h, "/auth/login", authBody(t, "", ""), "")
	if bos.Code != http.StatusUnauthorized {
		t.Fatalf("bos alan durum = %d, beklenen 401", bos.Code)
	}
	assertErrorBody(t, bos, "invalid_credentials")
}

func TestAuthMeUnauthorized(t *testing.T) {
	h, _ := authRouter(t)

	for ad, header := range map[string]string{
		"yok":         "",
		"bearer yok":  "Bearer",
		"baska sema":  "Basic abc",
		"fazla parca": "Bearer abc def",
	} {
		t.Run(ad, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("durum = %d, beklenen 401", rec.Code)
			}
			assertErrorBody(t, rec, "unauthorized")
		})
	}
}

func TestAuthMeInvalidToken(t *testing.T) {
	h, _ := authRouter(t)
	rec := authGet(t, h, "/auth/me", "gecersiz-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")
}

func TestAuthMeExpired(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	token := benzersiz("suresi-dolmus-token")
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO auth_sessions (user_id, token_hash, created_at, expires_at)
		 VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')`,
		user.ID, auth.HashToken(token),
	); err != nil {
		t.Fatalf("suresi dolmus oturum: %v", err)
	}

	rec := authGet(t, h, "/auth/me", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "unauthorized")
}

func TestAuthMeSuccess(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleAccountant, true)
	giris := authLogin(t, h, user.Email, authTestParola)

	rec := authGet(t, h, "/auth/me", giris.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var view userView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("govde cozulemedi: %v", err)
	}
	if view.ID != user.ID || view.Email != user.Email || view.Role != auth.RoleAccountant || !view.Active {
		t.Fatalf("me alanlari yanlis: %+v", view)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatal("me yanitinda parola alani var")
	}
}

func TestAuthMeInactive(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	giris := authLogin(t, h, user.Email, authTestParola)
	if _, err := pool.Exec(context.Background(), `UPDATE users SET active = false WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("kullanici pasiflestirilemedi: %v", err)
	}

	rec := authGet(t, h, "/auth/me", giris.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")
}

func TestAuthLogoutRevokes(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	giris := authLogin(t, h, user.Email, authTestParola)

	rec := authPost(t, h, "/auth/logout", nil, giris.Token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("durum = %d, beklenen 204; govde: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 govdesi bos olmali: %q", rec.Body.String())
	}
	if me := authGet(t, h, "/auth/me", giris.Token); me.Code != http.StatusUnauthorized {
		t.Fatalf("logout sonrasi me durum = %d, beklenen 401", me.Code)
	}
	if tekrar := authPost(t, h, "/auth/logout", nil, giris.Token); tekrar.Code != http.StatusUnauthorized {
		t.Fatalf("ikinci logout durum = %d, beklenen 401", tekrar.Code)
	}
	if auditSayisi(t, pool, "auth.logout") < 1 {
		t.Fatal("auth.logout denetim kaydi yok")
	}
	if rec := authPost(t, h, "/auth/logout", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("tokensiz logout durum = %d, beklenen 401", rec.Code)
	}
}

func TestAuthAdminUsersAdmin(t *testing.T) {
	h, pool := authRouter(t)
	admin := authKullanici(t, pool, auth.RoleAdmin, true)
	giris := authLogin(t, h, admin.Email, authTestParola)

	rec := authGet(t, h, "/admin/users", giris.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d; govde: %s", rec.Code, rec.Body.String())
	}
	var body adminUsersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("govde cozulemedi: %v", err)
	}
	if len(body.Items) == 0 || len(body.Items) > 20 {
		t.Fatalf("items sayisi = %d", len(body.Items))
	}
	bulundu := false
	for _, item := range body.Items {
		if item.ID == admin.ID {
			bulundu = true
		}
	}
	if !bulundu {
		t.Fatal("admin kullanicisi listede yok")
	}
	if body.NextAfterID != nil && len(body.Items) != 20 {
		t.Fatalf("next_after_id var ama items = %d", len(body.Items))
	}
}

func TestAuthAdminUsersForbidden(t *testing.T) {
	h, pool := authRouter(t)
	for _, role := range []auth.Role{auth.RoleDriver, auth.RoleAttendant, auth.RoleAccountant, auth.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			user := authKullanici(t, pool, role, true)
			giris := authLogin(t, h, user.Email, authTestParola)
			rec := authGet(t, h, "/admin/users", giris.Token)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("durum = %d, beklenen 403; govde: %s", rec.Code, rec.Body.String())
			}
			assertErrorBody(t, rec, "forbidden")
		})
	}
}

func TestAuthAdminUsersUnauthorized(t *testing.T) {
	h, _ := authRouter(t)
	rec := authGet(t, h, "/admin/users", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")
}

func TestAuthAdminUsersPagination(t *testing.T) {
	h, pool := authRouter(t)
	admin := authKullanici(t, pool, auth.RoleAdmin, true)
	giris := authLogin(t, h, admin.Email, authTestParola)

	ilk := authGet(t, h, "/admin/users?limit=2", giris.Token)
	if ilk.Code != http.StatusOK {
		t.Fatalf("ilk sayfa durum = %d; govde: %s", ilk.Code, ilk.Body.String())
	}
	var sayfa1 adminUsersResponse
	if err := json.Unmarshal(ilk.Body.Bytes(), &sayfa1); err != nil {
		t.Fatalf("ilk sayfa cozulemedi: %v", err)
	}
	if len(sayfa1.Items) != 2 {
		t.Fatalf("ilk sayfa items = %d, beklenen 2", len(sayfa1.Items))
	}
	if sayfa1.NextAfterID == nil {
		t.Fatal("ilk sayfada next_after_id yok")
	}
	if *sayfa1.NextAfterID != sayfa1.Items[1].ID {
		t.Fatalf("next_after_id = %d, son oge = %d", *sayfa1.NextAfterID, sayfa1.Items[1].ID)
	}

	ikinci := authGet(t, h, fmt.Sprintf("/admin/users?after_id=%d&limit=2", *sayfa1.NextAfterID), giris.Token)
	if ikinci.Code != http.StatusOK {
		t.Fatalf("ikinci sayfa durum = %d; govde: %s", ikinci.Code, ikinci.Body.String())
	}
	var sayfa2 adminUsersResponse
	if err := json.Unmarshal(ikinci.Body.Bytes(), &sayfa2); err != nil {
		t.Fatalf("ikinci sayfa cozulemedi: %v", err)
	}
	if len(sayfa2.Items) == 0 {
		t.Fatal("ikinci sayfa bos")
	}
	for _, item := range sayfa2.Items {
		if item.ID <= *sayfa1.NextAfterID {
			t.Fatalf("ikinci sayfada keyset ihlali: %d <= %d", item.ID, *sayfa1.NextAfterID)
		}
		for _, onceki := range sayfa1.Items {
			if item.ID == onceki.ID {
				t.Fatalf("ayni kullanici iki sayfada: %d", item.ID)
			}
		}
	}

	for _, sorgu := range []string{"after_id=abc", "after_id=-1", "limit=0", "limit=101", "limit=abc"} {
		rec := authGet(t, h, "/admin/users?"+sorgu, giris.Token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s durum = %d, beklenen 400", sorgu, rec.Code)
		}
		assertErrorBody(t, rec, "invalid_pagination")
	}
}

func TestAuthAdminUsersNoPasswordHash(t *testing.T) {
	h, pool := authRouter(t)
	admin := authKullanici(t, pool, auth.RoleAdmin, true)
	giris := authLogin(t, h, admin.Email, authTestParola)

	rec := authGet(t, h, "/admin/users?limit=100", giris.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("yanitta parola alani var: %s", rec.Body.String())
	}
}

func TestAuthLoginFailedAudit(t *testing.T) {
	h, pool := authRouter(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	gizliParola := "gizli-parola-audit-123"

	before := auditSayisi(t, pool, "auth.login_failed")
	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, gizliParola), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d", rec.Code)
	}
	if after := auditSayisi(t, pool, "auth.login_failed"); after <= before {
		t.Fatalf("denetim kaydi artmadi: %d -> %d", before, after)
	}
	var kayit string
	if err := pool.QueryRow(context.Background(),
		`SELECT coalesce(new_value::text, '') || ' ' || coalesce(old_value::text, '')
		 FROM audit_log WHERE action = 'auth.login_failed' ORDER BY id DESC LIMIT 1`,
	).Scan(&kayit); err != nil {
		t.Fatalf("denetim okunamadi: %v", err)
	}
	if strings.Contains(kayit, gizliParola) || !strings.Contains(kayit, user.Email) {
		t.Fatalf("denetim kaydi beklenen icerikte degil: %s", kayit)
	}
}

func TestAuthLogsNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pool := testdb.Open(t)
	h := NewRouter(Deps{
		Logger:      logger,
		Pinger:      pool,
		PingTimeout: time.Second,
		DB:          pool,
		SessionTTL:  time.Hour,
	})
	user := authKullanici(t, pool, auth.RoleDriver, true)

	giris := authLogin(t, h, user.Email, authTestParola)
	gizli := "logda-gorunmemeli-parola"
	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, gizli), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d", rec.Code)
	}

	log := buf.String()
	if strings.Contains(log, giris.Token) {
		t.Fatalf("token loga yazildi: %s", log)
	}
	if strings.Contains(log, authTestParola) || strings.Contains(log, gizli) {
		t.Fatalf("parola loga yazildi: %s", log)
	}
}

func TestAuthLoginDatabaseError(t *testing.T) {
	h := NewRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second, DB: hataQuerier{}})
	rec := authPost(t, h, "/auth/login", authBody(t, "a@b.c", "x"), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	assertErrorBody(t, rec, "internal")

	nilDB := NewRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second})
	rec = authPost(t, nilDB, "/auth/login", authBody(t, "a@b.c", "x"), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil DB durum = %d, beklenen 500", rec.Code)
	}
}

func TestAuthMiddlewareDatabaseError(t *testing.T) {
	h := NewRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second, DB: hataQuerier{}})
	rec := authGet(t, h, "/auth/me", "token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401", rec.Code)
	}
	assertErrorBody(t, rec, "unauthorized")

	nilDB := NewRouter(Deps{Logger: discardLogger(), Pinger: fakePinger{}, PingTimeout: time.Second})
	rec = authGet(t, nilDB, "/auth/me", "token")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil DB durum = %d, beklenen 500", rec.Code)
	}
}

func TestAuthSessionCreateFailure(t *testing.T) {
	pool := testdb.Open(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	h := NewRouter(Deps{
		Logger:      discardLogger(),
		Pinger:      pool,
		PingTimeout: time.Second,
		DB:          sessionHataQuerier{Querier: pool},
		SessionTTL:  time.Hour,
	})
	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, authTestParola), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500; govde: %s", rec.Code, rec.Body.String())
	}
	assertErrorBody(t, rec, "internal")
}

func TestAuthAuditFailure(t *testing.T) {
	pool := testdb.Open(t)
	user := authKullanici(t, pool, auth.RoleDriver, true)
	h := NewRouter(Deps{
		Logger:      discardLogger(),
		Pinger:      pool,
		PingTimeout: time.Second,
		DB:          auditHataQuerier{Querier: pool},
		SessionTTL:  time.Hour,
	})
	rec := authPost(t, h, "/auth/login", authBody(t, user.Email, authTestParola), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("basarili giris denetim hatasi durum = %d, beklenen 500", rec.Code)
	}

	rec = authPost(t, h, "/auth/login", authBody(t, user.Email, "yanlis"), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("basarisiz giris denetim hatasi durum = %d, beklenen 401", rec.Code)
	}

	token, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	sessionID, err := store.CreateAuthSession(context.Background(), pool, user.ID, hash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("oturum: %v", err)
	}
	logout := auditHataQuerier{Querier: pool}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = req.WithContext(context.WithValue(req.Context(), authInfoKey{}, authInfo{User: user, SessionID: sessionID}))
	rec = httptest.NewRecorder()
	logoutHandler(Deps{Logger: discardLogger(), DB: logout})(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout denetim hatasi durum = %d, beklenen 500", rec.Code)
	}
}

func TestAuthAdminUsersDatabaseError(t *testing.T) {
	h := adminUsersHandler(Deps{Logger: discardLogger(), DB: hataQuerier{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = req.WithContext(context.WithValue(req.Context(), authInfoKey{}, authInfo{User: store.User{ID: 1, Role: auth.RoleAdmin}, SessionID: 1}))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	assertErrorBody(t, rec, "internal")
}

func TestAuthLogoutRevokeFailure(t *testing.T) {
	h := logoutHandler(Deps{Logger: discardLogger(), DB: hataQuerier{}})
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req = req.WithContext(context.WithValue(req.Context(), authInfoKey{}, authInfo{User: store.User{ID: 1}, SessionID: 1}))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("durum = %d, beklenen 500", rec.Code)
	}
	assertErrorBody(t, rec, "internal")
}

type hataRow struct{}

func (hataRow) Scan(dest ...any) error {
	return errors.New("veritabani hatasi")
}

type hataQuerier struct{}

func (hataQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("veritabani hatasi")
}

func (hataQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("veritabani hatasi")
}

func (hataQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return hataRow{}
}

type sessionHataQuerier struct {
	store.Querier
}

func (q sessionHataQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO auth_sessions") {
		return hataRow{}
	}
	return q.Querier.QueryRow(ctx, sql, args...)
}

type auditHataQuerier struct {
	store.Querier
}

func (q auditHataQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "audit_log") {
		return pgconn.CommandTag{}, errors.New("denetim yazilamadi")
	}
	return q.Querier.Exec(ctx, sql, args...)
}
