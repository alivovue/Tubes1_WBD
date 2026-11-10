// Package validation defines errors for invalid transport and scalar inputs.
// It has no knowledge of identities, repositories, or authorization policy.
package validation

import "errors"

var ErrInvalid = errors.New("input tidak valid")
