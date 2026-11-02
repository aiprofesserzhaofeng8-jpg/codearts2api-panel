package access

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const PasswordHashPrefix = "pbkdf2-sha256$600000$"

func HashAdminPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return "", err
	}
	return PasswordHashPrefix + hex.EncodeToString(salt) + "$" + hex.EncodeToString(key), nil
}
func VerifyAdminPassword(stored, password string) bool {
	if stored == "" || password == "" || len(password) > 4096 {
		return false
	}
	if !strings.HasPrefix(stored, "pbkdf2-") {
		a := sha256.Sum256([]byte(stored))
		b := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(a[:], b[:]) == 1
	}
	if !strings.HasPrefix(stored, PasswordHashPrefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(stored, PasswordHashPrefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, e1 := hex.DecodeString(parts[0])
	expected, e2 := hex.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}
