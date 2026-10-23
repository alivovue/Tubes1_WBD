package access

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordPolicy(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", 73)} {
		if _, err := HashPassword(password); !errors.Is(err, ErrInvalid) {
			t.Fatalf("password length %d accepted", len(password))
		}
	}
	password := "a-long-enough-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != 12 {
		t.Fatal("bcrypt cost must be 12")
	}
	if hash == password || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("invalid password hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("incorrect-password")) == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestPermissionMatrix(t *testing.T) {
	for _, test := range []struct {
		grant, required Permission
		allowed         bool
	}{
		{NoAccess, ViewOnly, false}, {NoAccess, ViewEdit, false}, {ViewOnly, ViewOnly, true}, {ViewOnly, ViewEdit, false}, {ViewEdit, ViewOnly, true}, {ViewEdit, ViewEdit, true}, {ViewEdit, NoAccess, false}, {Permission("admin"), ViewEdit, false},
	} {
		if got := test.grant.Allows(test.required); got != test.allowed {
			t.Errorf("%s allows %s = %v", test.grant, test.required, got)
		}
	}
}

func TestCredentialEncryption(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	a, err := encryptSecret(key, "target-secret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := encryptSecret(key, "target-secret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, []byte("target-secret")) {
		t.Fatal("encryption must use random nonces")
	}
	plain, err := decryptSecret(key, a)
	if err != nil || plain != "target-secret" {
		t.Fatal("round trip failed")
	}
	a[len(a)-1] ^= 1
	if _, err = decryptSecret(key, a); err == nil {
		t.Fatal("tampered credential accepted")
	}
	if _, err = decryptSecret(bytes.Repeat([]byte{8}, 32), b); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = decryptSecret(key, []byte("short")); err == nil {
		t.Fatal("malformed ciphertext accepted")
	}
}

func TestTokenAndConfig(t *testing.T) {
	a, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 43 || a == b || bytes.Equal(tokenHash(a), []byte(a)) {
		t.Fatal("invalid opaque tokens")
	}
	c := DefaultConfig(make([]byte, 32), true)
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.GracePeriod = c.RotationInterval
	if c.validate() == nil {
		t.Fatal("invalid rotation policy accepted")
	}
	c = DefaultConfig(make([]byte, 1), true)
	if c.validate() == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestLoginRateLimit(t *testing.T) {
	l := loginLimiter{entries: make(map[string]attempt)}
	now := time.Now()
	for n := 0; n < 15; n++ {
		if !l.allow("127.0.0.1", now) {
			t.Fatal("request limited early")
		}
	}
	if l.allow("127.0.0.1", now) {
		t.Fatal("excess attempt accepted")
	}
	if !l.allow("127.0.0.2", now) || !l.allow("127.0.0.1", now.Add(5*time.Minute)) {
		t.Fatal("independent client or new window rejected")
	}
}
