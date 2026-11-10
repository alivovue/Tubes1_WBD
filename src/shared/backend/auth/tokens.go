// Package auth implements cryptographic mechanisms without owning identity,
// session storage, authorization decisions, configuration, or secrets.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"
)

func RandomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func TokenHash(raw string) []byte { sum := sha256.Sum256([]byte(raw)); return sum[:] }
func Sign(key []byte, purpose, value string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(purpose + "\x00" + value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func RotationToken(key, previousHash []byte) string {
	return Sign(key, "session-rotation", base64.RawURLEncoding.EncodeToString(previousHash))
}
func MinTime(first, second time.Time) time.Time {
	if first.Before(second) {
		return first
	}
	return second
}
