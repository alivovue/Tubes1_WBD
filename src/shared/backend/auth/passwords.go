package auth

import (
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
	"schakker_shared/validation"
)

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 72 {
		return "", fmt.Errorf("%w: password harus 12–72 byte", validation.ErrInvalid)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

var dummyHash = sync.OnceValues(func() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("dummy-password-not-an-account"), 12)
})

func DummyPasswordHash() (string, error) { hash, err := dummyHash(); return string(hash), err }
