package httpapi

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"schakker_shared/validation"
)

func ID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, validation.ErrInvalid
	}
	return id, nil
}
func Offset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 100000000 {
		return 0, validation.ErrInvalid
	}
	return n, nil
}
func DecodeStrictJSON(raw string, value any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return validation.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return validation.ErrInvalid
	}
	return nil
}
