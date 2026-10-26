package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("dogru-parola")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "dogru-parola" {
		t.Fatalf("gecersiz ozet: %q", hash)
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("bcrypt maliyeti = %d (%v), beklenen %d", cost, err, bcrypt.DefaultCost)
	}
	if !CheckPassword(hash, "dogru-parola") {
		t.Fatal("dogru parola kabul edilmedi")
	}
}

func TestCheckPasswordWrong(t *testing.T) {
	hash, err := HashPasswordCost("dogru-parola", bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPasswordCost: %v", err)
	}
	if CheckPassword(hash, "yanlis-parola") {
		t.Fatal("yanlis parola kabul edildi")
	}
	if CheckPassword("bozuk-ozet", "dogru-parola") {
		t.Fatal("bozuk ozet kabul edildi")
	}
}

func TestGenerateTokenHash(t *testing.T) {
	token, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token base64url degil: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("token uzunlugu = %d, beklenen 32", len(raw))
	}
	if len(hash) != 32 {
		t.Fatalf("ozet uzunlugu = %d, beklenen 32", len(hash))
	}
	beklenen := sha256.Sum256([]byte(token))
	if !bytes.Equal(hash, beklenen[:]) {
		t.Fatal("token ozeti SHA-256 degil")
	}
	if !bytes.Equal(hash, HashToken(token)) {
		t.Fatal("HashToken ayni ozeti uretmedi")
	}

	token2, hash2, err := GenerateToken()
	if err != nil {
		t.Fatalf("ikinci GenerateToken: %v", err)
	}
	if token == token2 || bytes.Equal(hash, hash2) {
		t.Fatal("iki token ayni")
	}
}

func TestRoleValid(t *testing.T) {
	for _, role := range []Role{RoleDriver, RoleAttendant, RoleAccountant, RoleManager, RoleAdmin} {
		if !role.Valid() {
			t.Errorf("%q gecerli olmali", role)
		}
	}
	if Role("baskasi").Valid() {
		t.Error("bilinmeyen rol gecerli sayildi")
	}
}
