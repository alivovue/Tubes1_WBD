package migration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

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
	ErrInvalid             = access.ErrInvalid
	ErrForbidden           = access.ErrForbidden
	ErrStale               = errors.New("migration review is stale; analyze the current SQL again")
	ErrExecution           = errors.New("migration execution failed")
	ErrOutcomeUnknown      = engines.ErrOutcomeUnknown
	ErrHistoryPending      = errors.New("migration history requires reconciliation")
	ErrNonAtomicAcceptance = errors.New("explicit acceptance is required for a non-atomic migration")
)

type Service struct {
	store     *dao.Store
	access    *access.Service
	databases *dbmanagement.Service
	engines   *engines.Registry
}
type DraftInput struct {
	ID                int64
	Title             string
	SQL               string
	ExpectedUpdatedAt time.Time
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
	Signature             string
}
type ExecuteInput struct {
	Review          Analysis
	Description     string
	DraftID         int64
	AcceptNonAtomic bool
}

func NewService(store *dao.Store, auth *access.Service, databases *dbmanagement.Service) (*Service, error) {
	if store == nil || auth == nil || databases == nil {
		return nil, ErrInvalid
	}
	return &Service{store: store, access: auth, databases: databases, engines: engines.NewRegistry(pgengine.New(), mariaengine.New())}, nil
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
func (s *Service) signature(a Analysis) string {
	return s.databases.SignReview(fmt.Sprintf("%d\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s", a.DatabaseID, a.SQLHash, a.BaseVersion, a.SchemaFingerprint, a.ConnectionFingerprint, a.DBMS, a.Mode))
}
func (s *Service) SaveDraft(ctx context.Context, actor, database int64, input DraftInput) (dao.MigrationDraft, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return dao.MigrationDraft{}, err
	}
	if input.ID < 0 || strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 {
		return dao.MigrationDraft{}, ErrInvalid
	}
	engine, err := s.engine(ctx, actor, database)
	if err != nil {
		return dao.MigrationDraft{}, err
	}
	if _, err := engine.Parse(input.SQL); err != nil {
		return dao.MigrationDraft{}, err
	}
	if input.ID == 0 {
		return s.store.CreateDraft(ctx, database, actor, strings.TrimSpace(input.Title), input.SQL)
	}
	if input.ExpectedUpdatedAt.IsZero() {
		return dao.MigrationDraft{}, ErrInvalid
	}
	return s.store.UpdateDraft(ctx, database, input.ID, strings.TrimSpace(input.Title), input.SQL, input.ExpectedUpdatedAt)
}

func (s *Service) Draft(ctx context.Context, actor, database, id int64) (dao.MigrationDraft, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return dao.MigrationDraft{}, err
	}
	if id < 1 {
		return dao.MigrationDraft{}, ErrInvalid
	}
	return s.store.Draft(ctx, database, id)
}

func (s *Service) Drafts(ctx context.Context, actor, database int64, limit, offset int) ([]dao.MigrationDraft, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, ErrInvalid
	}
	return s.store.Drafts(ctx, database, limit, offset)
}

func (s *Service) DeleteDraft(ctx context.Context, actor, database, id int64) error {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return err
	}
	if id < 1 {
		return ErrInvalid
	}
	return s.store.DeleteDraft(ctx, database, id)
}

func (s *Service) History(ctx context.Context, actor, database int64, limit, offset int) ([]dao.MigrationExecution, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, ErrInvalid
	}
	return s.store.MigrationHistory(ctx, database, limit, offset)
}

func (s *Service) Detail(ctx context.Context, actor, database, id int64) (dao.MigrationExecution, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return dao.MigrationExecution{}, err
	}
	if id < 1 {
		return dao.MigrationExecution{}, ErrInvalid
	}
	return s.store.Execution(ctx, database, id)
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
	a.Signature = s.signature(a)
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
func (s *Service) validateReview(database int64, a Analysis) ([]Statement, error) {
	if database != a.DatabaseID || a.BaseVersion < 0 || a.SQLHash != SQLHash(a.SQL) || a.SchemaFingerprint == "" || a.ConnectionFingerprint == "" || !hmac.Equal([]byte(a.Signature), []byte(s.signature(a))) {
		return nil, ErrStale
	}
	engine, err := s.engines.Select(a.DBMS)
	if err != nil {
		return nil, err
	}
	return engine.Parse(a.SQL)
}
