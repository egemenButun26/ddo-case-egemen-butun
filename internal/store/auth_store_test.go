package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"parkakil/internal/auth"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func yeniKullanici(t *testing.T, role auth.Role, active bool) (int64, string) {
	t.Helper()
	pool := testdb.Open(t)
	email := strings.ToLower(benzersiz("kullanici")) + "@test.local"
	id, err := store.CreateUser(context.Background(), pool, store.CreateUserParams{
		Email:        email,
		Name:         "Test Kullanici",
		Role:         role,
		PasswordHash: "hash",
		Active:       &active,
	})
	if err != nil {
		t.Fatalf("kullanici olusturulamadi: %v", err)
	}
	return id, email
}

func TestUserCRUD(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	email := strings.ToLower(benzersiz("crud")) + "@test.local"
	plate := "34TEST01"

	id, err := store.CreateUser(ctx, pool, store.CreateUserParams{
		Email:        email,
		Name:         "CRUD Kullanici",
		Role:         auth.RoleDriver,
		PasswordHash: "hash",
		Plate:        &plate,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	byEmail, err := store.GetUserByEmail(ctx, pool, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if byEmail.ID != id || byEmail.Role != auth.RoleDriver || !byEmail.Active || byEmail.Plate == nil || *byEmail.Plate != plate {
		t.Fatalf("kullanici alanlari yanlis: %+v", byEmail)
	}

	byID, err := store.GetUser(ctx, pool, id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if byID.Email != email {
		t.Fatalf("email = %q, beklenen %q", byID.Email, email)
	}

	if _, err := store.GetUser(ctx, pool, 1<<60); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan kullanici hatasi = %v, beklenen ErrNotFound", err)
	}
	if _, err := store.GetUserByEmail(ctx, pool, "yok@test.local"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan e-posta hatasi = %v, beklenen ErrNotFound", err)
	}

	users, err := store.ListUsers(ctx, pool, 0, 1000)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	bulundu := false
	for _, u := range users {
		if u.ID == id {
			bulundu = true
		}
	}
	if !bulundu {
		t.Fatal("ListUsers olusturulan kullaniciyi dondurmedi")
	}

	if _, err := store.CreateUser(ctx, pool, store.CreateUserParams{
		Email:        email,
		Name:         "Tekrar",
		Role:         auth.RoleDriver,
		PasswordHash: "hash",
	}); err == nil {
		t.Fatal("ayni e-posta ikinci kez kabul edildi")
	}
}

func TestUserEmailUnique(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	email := strings.ToLower(benzersiz("tekrar")) + "@test.local"

	if _, err := store.CreateUser(ctx, pool, store.CreateUserParams{
		Email: email, Name: "Bir", Role: auth.RoleDriver, PasswordHash: "hash",
	}); err != nil {
		t.Fatalf("ilk kullanici: %v", err)
	}
	_, err := store.CreateUser(ctx, pool, store.CreateUserParams{
		Email: email, Name: "Iki", Role: auth.RoleDriver, PasswordHash: "hash",
	})
	if pgKodu(err) != "23505" {
		t.Fatalf("tekrar e-posta hatasi = %v (kod %q), beklenen 23505", err, pgKodu(err))
	}

	for _, kotu := range []string{"", "BUYUK@test.local"} {
		_, err := store.CreateUser(ctx, pool, store.CreateUserParams{
			Email: kotu, Name: "Kotu", Role: auth.RoleDriver, PasswordHash: "hash",
		})
		if pgKodu(err) != "23514" {
			t.Fatalf("gecersiz e-posta %q hatasi = %v (kod %q), beklenen 23514", kotu, err, pgKodu(err))
		}
	}

	if err := store.CreateUserIfMissing(ctx, pool, store.CreateUserParams{
		Email: email, Name: "Tekrar", Role: auth.RoleAdmin, PasswordHash: "hash2",
	}); err != nil {
		t.Fatalf("CreateUserIfMissing var olan kayitta hata verdi: %v", err)
	}
	u, err := store.GetUserByEmail(ctx, pool, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.Name != "Bir" || u.Role != auth.RoleDriver {
		t.Fatalf("CreateUserIfMissing var olan kaydi degistirdi: %+v", u)
	}
}

func TestUserRoleCheck(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	_, err := store.CreateUser(ctx, pool, store.CreateUserParams{
		Email:        strings.ToLower(benzersiz("rol")) + "@test.local",
		Name:         "Gecersiz",
		Role:         auth.Role("baskasi"),
		PasswordHash: "hash",
	})
	if pgKodu(err) != "23514" {
		t.Fatalf("gecersiz rol hatasi = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}
}

func TestAuthSessionValidity(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	userID, _ := yeniKullanici(t, auth.RoleDriver, true)

	tokenHash := auth.HashToken(benzersiz("token"))
	id, err := store.CreateAuthSession(ctx, pool, userID, tokenHash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateAuthSession: %v", err)
	}
	oturum, err := store.GetAuthSessionByTokenHash(ctx, pool, tokenHash)
	if err != nil {
		t.Fatalf("GetAuthSessionByTokenHash: %v", err)
	}
	if oturum.ID != id || oturum.UserID != userID || oturum.RevokedAt != nil {
		t.Fatalf("oturum alanlari yanlis: %+v", oturum)
	}

	if err := store.RevokeAuthSession(ctx, pool, id); err != nil {
		t.Fatalf("RevokeAuthSession: %v", err)
	}
	if _, err := store.GetAuthSessionByTokenHash(ctx, pool, tokenHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke sonrasi hata = %v, beklenen ErrNotFound", err)
	}

	suresiDolmus := auth.HashToken(benzersiz("suresi-dolmus"))
	if _, err := pool.Exec(ctx,
		`INSERT INTO "ya-auth_sessions" (user_id, token_hash, created_at, expires_at)
		 VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')`,
		userID, suresiDolmus,
	); err != nil {
		t.Fatalf("suresi dolmus oturum eklenemedi: %v", err)
	}
	if _, err := store.GetAuthSessionByTokenHash(ctx, pool, suresiDolmus); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("suresi dolmus oturum hatasi = %v, beklenen ErrNotFound", err)
	}

	if _, err := store.GetAuthSessionByTokenHash(ctx, pool, auth.HashToken("yok")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bilinmeyen token hatasi = %v, beklenen ErrNotFound", err)
	}

	kisaHash := []byte{1, 2, 3}
	if _, err := store.CreateAuthSession(ctx, pool, userID, kisaHash, time.Now().Add(time.Hour)); pgKodu(err) != "23514" {
		t.Fatalf("kisa ozet hatasi = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}
}

func TestAuditAppendOnly(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	userID, _ := yeniKullanici(t, auth.RoleAdmin, true)

	entry := store.AuditEntry{
		ActorUserID: &userID,
		ActorKind:   store.ActorUser,
		Action:      "test.action",
		Entity:      "user",
		EntityID:    "42",
		OldValue:    map[string]any{"role": "driver"},
		NewValue:    map[string]any{"role": "admin"},
		RequestID:   "req-1",
	}
	if err := store.AppendAudit(ctx, pool, entry); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}

	var id int64
	var oldRole, newRole, requestID string
	if err := pool.QueryRow(ctx,
		`SELECT id, old_value->>'role', new_value->>'role', request_id
		 FROM "ya-audit_log" WHERE action = 'test.action' AND entity_id = '42'
		 ORDER BY id DESC LIMIT 1`,
	).Scan(&id, &oldRole, &newRole, &requestID); err != nil {
		t.Fatalf("denetim kaydi okunamadi: %v", err)
	}
	if oldRole != "driver" || newRole != "admin" || requestID != "req-1" {
		t.Fatalf("denetim degerleri yanlis: %s %s %s", oldRole, newRole, requestID)
	}

	if _, err := pool.Exec(ctx, `UPDATE "ya-audit_log" SET action = 'degisti' WHERE id = $1`, id); err == nil {
		t.Fatal("audit_log UPDATE kabul edildi")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE hatasi append-only icermeli: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "ya-audit_log" WHERE id = $1`, id); err == nil {
		t.Fatal("audit_log DELETE kabul edildi")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE hatasi append-only icermeli: %v", err)
	}

	if err := store.AppendAudit(ctx, pool, store.AuditEntry{
		ActorKind: store.ActorSystem,
		Action:    "test.action",
		Entity:    "test",
		EntityID:  "1",
		NewValue:  make(chan int),
	}); err == nil {
		t.Fatal("kodlanamayan denetim degeri kabul edildi")
	}

	if err := store.AppendAudit(ctx, pool, store.AuditEntry{
		ActorKind: "baskasi",
		Action:    "test.action",
		Entity:    "test",
		EntityID:  "2",
	}); pgKodu(err) != "23514" {
		t.Fatalf("gecersiz actor_kind hatasi = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}
}
