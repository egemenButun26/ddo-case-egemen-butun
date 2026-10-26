package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleDriver     Role = "driver"
	RoleAttendant  Role = "attendant"
	RoleAccountant Role = "accountant"
	RoleManager    Role = "manager"
	RoleAdmin      Role = "admin"
)

func (r Role) Valid() bool {
	switch r {
	case RoleDriver, RoleAttendant, RoleAccountant, RoleManager, RoleAdmin:
		return true
	default:
		return false
	}
}

func HashPassword(password string) (string, error) {
	return HashPasswordCost(password, bcrypt.DefaultCost)
}

func HashPasswordCost(password string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GenerateToken() (string, []byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
