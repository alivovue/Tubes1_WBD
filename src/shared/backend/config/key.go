// Package config parses supplied values. Callers own environment names, policy,
// defaults, key sources, and lifecycles. This package never reads or writes files.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
)

func Key32(encoded string) ([]byte, error) {
	key, err := hex.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("key must contain 32 bytes encoded as hex or base64")
	}
	return key, nil
}
