package engines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

const Transactional = "transactional"
const NonAtomic = "non_atomic"
const Reconcile = "reconcile"

var ErrOutcomeUnknown = errors.New("migration outcome is unconfirmed; reconcile history before retrying")
var ErrUnsupported = adapters.ErrUnsupported
var ErrUnsafeSQL = fmt.Errorf("%w: migration SQL uses protected or dynamic operations", adapters.ErrInvalid)
var ErrProtectedMetadata = errors.New("protected migration metadata changed or could not be verified")

type Receipt struct {
	Namespace           string
	AttemptID           int64
	Version             int64
	SQLHash             string
	Status              string
	Mode                string
	AppliedAt           time.Time
	CompletedStatements int
	TotalStatements     int
}
type Engine interface {
	Name() string
	Parse(string) ([]adapters.Statement, error)
	Assess(context.Context, adapters.Querier, []adapters.Statement) (string, []string, error)
	Open(context.Context, *sql.DB, adapters.Adapter, string) (Session, error)
	SafeFailure(error) string
}
type Session interface {
	Fingerprint(context.Context) (string, error)
	Receipt(context.Context, string, int64) (Receipt, error)
	Prepare(context.Context, Receipt) error
	Execute(context.Context, adapters.Statement) error
	Commit(context.Context, Receipt) (Receipt, error)
	Rollback() error
	Close() error
	KnownFailure(error) bool
	Progress() (int, int)
	Resolve(context.Context, Receipt, bool) error
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
