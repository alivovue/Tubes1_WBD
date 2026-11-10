package config_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"schakker_shared/config"
)

func TestKey32(t *testing.T) {
	want := bytes.Repeat([]byte{0xa7}, 32)
	for _, encoded := range []string{hex.EncodeToString(want), base64.StdEncoding.EncodeToString(want)} {
		got, err := config.Key32(encoded)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("valid key rejected: %v", err)
		}
	}
	for _, encoded := range []string{"", "not-a-key", hex.EncodeToString(want[:31]), base64.StdEncoding.EncodeToString(want[:31])} {
		if _, err := config.Key32(encoded); err == nil {
			t.Fatal("accepted invalid or short key")
		}
	}
}
