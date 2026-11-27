package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"schakker_shared/observability"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/diff"
	"tubes1_wbd/backend/migration/engines"
	mariaengine "tubes1_wbd/backend/migration/engines/mariadb"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
	"tubes1_wbd/database/dao"
)

var (
	ErrInvalid   = access.ErrInvalid
	ErrForbidden = access.ErrForbidden
	ErrStale     = errors.New("migration review is stale; analyze the current SQL again")
	ErrUnsafeSQL = engines.ErrUnsafeSQL
)

type Service struct {
	store     *dao.Store
	access    *access.Service
	databases *dbmanagement.Service
	engines   *engines.Registry
	workflow  *Workflow
}
type Plan = adapters.Plan
type Statement = adapters.Statement
type Analysis struct {
	DatabaseID            int64
	DBMS                  string
	SQL                   string
	SQLHash               string
	BaseVersion           int64
	SchemaFingerprint     string
	ConnectionFingerprint string
	Plans                 []Plan
	Mode                  string
	Transactional         bool
	Warnings              []string
	SchemaDiffAvailable   bool
	SchemaDiffMessage     string
	SchemaDiff            *diff.Result
}

func NewService(store *dao.Store, auth *access.Service, databases *dbmanagement.Service) (*Service, error) {
	if store == nil || auth == nil || databases == nil {
		return nil, ErrInvalid
	}
	s := &Service{store: store, access: auth, databases: databases, engines: engines.NewRegistry(pgengine.New(), mariaengine.New())}
	s.workflow = NewWorkflow(store, auth, WorkflowOptions{})
	return s, nil
}

func (s *Service) Workflow() *Workflow { return s.workflow }

// ConfigureWorkflow wires P3's adapters before starting HTTP servers/workers.
func (s *Service) ConfigureWorkflow(options WorkflowOptions) {
	s.workflow = NewWorkflow(s.store, s.access, options)
}
func SQLHash(query string) string {
	sum := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sum[:])
}
func (s *Service) engine(ctx context.Context, actor, database int64) (engines.Engine, error) {
	dbms, err := s.databases.DBMS(ctx, actor, database)
	if err != nil {
		return nil, err
	}
	return s.engines.Select(dbms)
}
func ValidateSQL(query string, dbms ...string) ([]Statement, error) {
	name := "postgresql"
	if len(dbms) > 0 {
		name = dbms[0]
	}
	registry := engines.NewRegistry(pgengine.New(), mariaengine.New())
	engine, err := registry.Select(name)
	if err != nil {
		return nil, err
	}
	return engine.Parse(query)
}
func (s *Service) Analyze(ctx context.Context, actor, database int64, query string) (Analysis, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return Analysis{}, err
	}
	engine, err := s.engine(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	statements, err := engine.Parse(query)
	if err != nil {
		return Analysis{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := s.databases.ConnectionFingerprint(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	target, err := s.databases.OpenMigrationTarget(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	defer target.DB.Close()
	ctx = observability.WithSQLFormatter(ctx, func(query string) string { return adapters.SQLLogText(query, target.Adapter.Name()) })
	tx, err := target.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Analysis{}, dbmanagement.ErrConnection
	}
	defer tx.Rollback()
	mode, warnings, err := engine.Assess(ctx, tx, statements)
	if err != nil {
		return Analysis{}, err
	}
	version, err := s.store.MigrationVersion(ctx, database)
	if err != nil {
		return Analysis{}, err
	}
	fingerprint, err := target.Adapter.Fingerprint(ctx, tx)
	if err != nil {
		return Analysis{}, err
	}
	plans, err := target.Adapter.QueryPlans(ctx, tx, statements)
	if err != nil {
		return Analysis{}, err
	}
	if err := tx.Commit(); err != nil {
		return Analysis{}, dbmanagement.ErrQuery
	}
	currentConnection, err := s.databases.ConnectionFingerprint(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	if connection != currentConnection {
		return Analysis{}, ErrStale
	}
	a := Analysis{DatabaseID: database, DBMS: engine.Name(), SQL: query, SQLHash: SQLHash(query), BaseVersion: version, SchemaFingerprint: fingerprint, ConnectionFingerprint: connection, Plans: plans, Mode: mode, Transactional: mode == engines.Transactional, Warnings: warnings, SchemaDiffMessage: "Schema diff is unavailable for arbitrary SQL; use a desired schema or structured change set"}
	return a, nil
}
func (s *Service) AnalyzeDesired(ctx context.Context, actor, database int64, desired diff.Snapshot) (Analysis, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return Analysis{}, err
	}
	current, err := s.databases.Snapshot(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	result, err := diff.Compare(current, desired)
	if err != nil {
		return Analysis{}, err
	}
	return s.analyzeDiff(ctx, actor, database, current, result)
}
func (s *Service) AnalyzeChanges(ctx context.Context, actor, database int64, operations []diff.Operation) (Analysis, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return Analysis{}, err
	}
	current, err := s.databases.Snapshot(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	result, err := diff.Build(current, operations)
	if err != nil {
		return Analysis{}, err
	}
	return s.analyzeDiff(ctx, actor, database, current, result)
}
func (s *Service) analyzeDiff(ctx context.Context, actor, database int64, current diff.Snapshot, result diff.Result) (Analysis, error) {
	if result.SQL == "" {
		return Analysis{DatabaseID: database, DBMS: current.DBMS, SchemaDiffAvailable: true, SchemaDiff: &result}, diff.ErrNoChanges
	}
	a, err := s.Analyze(ctx, actor, database, result.SQL)
	if err != nil {
		return a, err
	}
	if a.SchemaFingerprint != current.Fingerprint {
		return Analysis{}, ErrStale
	}
	a.SchemaDiffAvailable = true
	a.SchemaDiffMessage = ""
	a.SchemaDiff = &result
	return a, nil
}
