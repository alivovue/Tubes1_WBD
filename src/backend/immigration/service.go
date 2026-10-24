package immigration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"
)

var (
	ErrInvalid        = access.ErrInvalid
	ErrForbidden      = access.ErrForbidden
	ErrStale          = errors.New("migration review is stale; analyze the current SQL again")
	ErrExecution      = errors.New("migration failed and was rolled back")
	ErrOutcomeUnknown = errors.New("migration outcome is unconfirmed; reconcile history before retrying")
	ErrHistoryPending = errors.New("migration history requires reconciliation")
)

type Service struct {
	store     *dao.Store
	access    *access.Service
	databases *dbmanagement.Service
}
type DraftInput struct {
	ID                int64
	Title             string
	SQL               string
	ExpectedUpdatedAt time.Time
}
type Plan struct {
	Statement string
	Available bool
	JSON      json.RawMessage
	Message   string
}
type Analysis struct {
	DatabaseID            int64
	SQL                   string
	SQLHash               string
	BaseVersion           int64
	SchemaFingerprint     string
	ConnectionFingerprint string
	Plans                 []Plan
	Signature             string
}
type ExecuteInput struct {
	Review      Analysis
	Description string
	DraftID     int64
}

func NewService(store *dao.Store, auth *access.Service, databases *dbmanagement.Service) (*Service, error) {
	if store == nil || auth == nil || databases == nil {
		return nil, ErrInvalid
	}
	return &Service{store: store, access: auth, databases: databases}, nil
}

func SQLHash(query string) string {
	sum := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sum[:])
}

func (s *Service) signature(a Analysis) string {
	return s.databases.SignReview(fmt.Sprintf("%d\x00%s\x00%d\x00%s\x00%s", a.DatabaseID, a.SQLHash, a.BaseVersion, a.SchemaFingerprint, a.ConnectionFingerprint))
}

func (s *Service) SaveDraft(ctx context.Context, actor, database int64, input DraftInput) (dao.MigrationDraft, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return dao.MigrationDraft{}, err
	}
	if input.ID < 0 || strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 {
		return dao.MigrationDraft{}, ErrInvalid
	}
	if _, err := ValidateSQL(input.SQL); err != nil {
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
	statements, err := ValidateSQL(query)
	if err != nil {
		return Analysis{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := s.databases.ConnectionFingerprint(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	db, err := s.databases.OpenForMigration(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Analysis{}, dbmanagement.ErrConnection
	}
	defer tx.Rollback()
	version, err := s.store.MigrationVersion(ctx, database)
	if err != nil {
		return Analysis{}, err
	}
	fingerprint, err := dbmanagement.SchemaFingerprint(ctx, tx)
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{DatabaseID: database, SQL: query, SQLHash: SQLHash(query), BaseVersion: version, SchemaFingerprint: fingerprint, ConnectionFingerprint: connection, Plans: make([]Plan, 0, len(statements))}
	for _, statement := range statements {
		plan := Plan{Statement: statement.SQL, Message: "Plan unavailable for this statement"}
		if statement.Explainable {
			if _, err := tx.ExecContext(ctx, `SAVEPOINT schakker_explain`); err != nil {
				return Analysis{}, dbmanagement.ErrQuery
			}
			var raw []byte
			if err := tx.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+statement.SQL).Scan(&raw); err == nil && json.Valid(raw) {
				plan.Available = true
				plan.JSON = json.RawMessage(append([]byte(nil), raw...))
				plan.Message = ""
			} else {
				plan.Message = "Plan unavailable against the current schema"
			}
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT schakker_explain`); err != nil {
				return Analysis{}, dbmanagement.ErrQuery
			}
			if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT schakker_explain`); err != nil {
				return Analysis{}, dbmanagement.ErrQuery
			}
		}
		a.Plans = append(a.Plans, plan)
	}
	if err := tx.Commit(); err != nil {
		return Analysis{}, dbmanagement.ErrQuery
	}
	currentConnection, err := s.databases.ConnectionFingerprint(ctx, actor, database)
	if err != nil {
		return Analysis{}, err
	}
	if currentConnection != connection {
		return Analysis{}, ErrStale
	}
	a.Signature = s.signature(a)
	return a, nil
}

func (s *Service) validateReview(database int64, a Analysis) ([]Statement, error) {
	if database != a.DatabaseID || a.BaseVersion < 0 || a.SQLHash != SQLHash(a.SQL) || a.SchemaFingerprint == "" || a.ConnectionFingerprint == "" || !hmac.Equal([]byte(a.Signature), []byte(s.signature(a))) {
		return nil, ErrStale
	}
	return ValidateSQL(a.SQL)
}
