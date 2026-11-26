package engines

import (
	"context"
	"errors"
	"fmt"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

const Transactional = "transactional"
const NonAtomic = "non_atomic"

var ErrUnsupported = adapters.ErrUnsupported
var ErrUnsafeSQL = fmt.Errorf("%w: migration SQL uses protected or dynamic operations", adapters.ErrInvalid)
var ErrProtectedMetadata = errors.New("protected migration metadata changed or could not be verified")

type Engine interface {
	Name() string
	Parse(string) ([]adapters.Statement, error)
	Assess(context.Context, adapters.Querier, []adapters.Statement) (string, []string, error)
	SafeFailure(error) string
}
type Registry struct{ items map[string]Engine }

func NewRegistry(items ...Engine) *Registry {
	r := &Registry{items: make(map[string]Engine)}
	for _, item := range items {
		r.items[item.Name()] = item
	}
	return r
}
func (r *Registry) Select(dbms string) (Engine, error) {
	value := r.items[adapters.Normalize(dbms)]
	if value == nil {
		return nil, ErrUnsupported
	}
	return value, nil
}
