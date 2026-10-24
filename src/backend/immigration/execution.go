package immigration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"

	"github.com/jackc/pgx/v5/pgconn"
)

type receipt struct {
	Version   int64
	SQLHash   string
	AppliedAt time.Time
}

func namespaceCandidate() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func lockTarget(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421108)`); err != nil {
		return dbmanagement.ErrQuery
	}
	return nil
}

func ensureReceipts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS _schakker; CREATE TABLE IF NOT EXISTS _schakker.migration_receipts(namespace text NOT NULL,attempt_id bigint NOT NULL,version bigint NOT NULL,sql_hash text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(namespace,attempt_id),UNIQUE(namespace,version))`)
	return err
}

func readReceipt(ctx context.Context, tx *sql.Tx, namespace string, id int64) (receipt, bool, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('_schakker.migration_receipts') IS NOT NULL`).Scan(&exists); err != nil {
		return receipt{}, false, err
	}
	if !exists {
		return receipt{}, false, nil
	}
	var result receipt
	err := tx.QueryRowContext(ctx, `SELECT version,sql_hash,applied_at FROM _schakker.migration_receipts WHERE namespace=$1 AND attempt_id=$2`, namespace, id).Scan(&result.Version, &result.SQLHash, &result.AppliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	return result, err == nil, err
}

func (s *Service) reconcileLocked(ctx context.Context, store *dao.Store, tx *sql.Tx, database int64, namespace string) error {
	pending, err := store.UnfinishedExecutions(ctx, database)
	if err != nil {
		return err
	}
	for _, execution := range pending {
		committed, exists, err := readReceipt(ctx, tx, namespace, execution.ID)
		if err != nil {
			return ErrOutcomeUnknown
		}
		if exists {
			if committed.Version != execution.AttemptedVersion || committed.SQLHash != SQLHash(execution.SQL) {
				return ErrHistoryPending
			}
			if err := store.FinishExecution(ctx, database, execution.ID, "succeeded", &committed.Version, "", committed.AppliedAt); err != nil {
				return ErrHistoryPending
			}
		} else {
			if err := store.FinishExecution(ctx, database, execution.ID, "failed", nil, "Interrupted migration did not commit", time.Now().UTC()); err != nil {
				return ErrHistoryPending
			}
		}
	}
	return nil
}

func (s *Service) Reconcile(ctx context.Context, actor, database int64) error {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return s.store.WithDatabaseLock(ctx, database, func(store *dao.Store) error {
		db, err := s.databases.OpenForMigration(ctx, actor, database)
		if err != nil {
			return err
		}
		defer db.Close()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return dbmanagement.ErrConnection
		}
		defer tx.Rollback()
		if err := lockTarget(ctx, tx); err != nil {
			return err
		}
		candidate, err := namespaceCandidate()
		if err != nil {
			return err
		}
		namespace, err := store.DatabaseNamespace(ctx, database, candidate)
		if err != nil {
			return err
		}
		return s.reconcileLocked(ctx, store, tx, database, namespace)
	})
}

func (s *Service) Execute(ctx context.Context, actor, database int64, input ExecuteInput) (dao.MigrationExecution, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return dao.MigrationExecution{}, err
	}
	if input.DraftID < 0 || len(input.Description) > 4000 {
		return dao.MigrationExecution{}, ErrInvalid
	}
	statements, err := s.validateReview(database, input.Review)
	if err != nil {
		return dao.MigrationExecution{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var execution dao.MigrationExecution
	err = s.store.WithDatabaseLock(ctx, database, func(store *dao.Store) error {
		connection, err := s.databases.ConnectionFingerprint(ctx, actor, database)
		if err != nil {
			return err
		}
		if connection != input.Review.ConnectionFingerprint {
			return ErrStale
		}
		db, err := s.databases.OpenForMigration(ctx, actor, database)
		if err != nil {
			return err
		}
		defer db.Close()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return dbmanagement.ErrConnection
		}
		defer tx.Rollback()
		if err := lockTarget(ctx, tx); err != nil {
			return err
		}
		candidate, err := namespaceCandidate()
		if err != nil {
			return err
		}
		namespace, err := store.DatabaseNamespace(ctx, database, candidate)
		if err != nil {
			return err
		}
		if err := s.reconcileLocked(ctx, store, tx, database, namespace); err != nil {
			return err
		}
		version, err := store.MigrationVersion(ctx, database)
		if err != nil {
			return err
		}
		fingerprint, err := dbmanagement.SchemaFingerprint(ctx, tx)
		if err != nil {
			return err
		}
		if version != input.Review.BaseVersion || fingerprint != input.Review.SchemaFingerprint {
			return ErrStale
		}
		var draftID *int64
		if input.DraftID > 0 {
			draft, err := store.Draft(ctx, database, input.DraftID)
			if err != nil {
				return err
			}
			if SQLHash(draft.SQL) != input.Review.SQLHash {
				return ErrStale
			}
			draftID = &input.DraftID
		}
		if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
			return err
		}
		execution, err = store.StartExecution(ctx, dao.MigrationExecution{DatabaseID: database, DraftID: draftID, ExecutedBy: actor, Description: strings.TrimSpace(input.Description), SQL: input.Review.SQL, AttemptedVersion: version + 1})
		if err != nil {
			return err
		}
		fail := func(cause error, message string) error {
			tx.Rollback()
			finished := time.Now().UTC()
			execution.Status = "failed"
			execution.ErrorMessage = message
			execution.FinishedAt = &finished
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			if err := store.FinishExecution(cleanup, database, execution.ID, "failed", nil, message, finished); err != nil {
				return ErrHistoryPending
			}
			return cause
		}
		if err := ensureReceipts(ctx, tx); err != nil {
			return fail(ErrExecution, safeFailure(err))
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, `SELECT set_config('standard_conforming_strings','on',true)`); err != nil {
				return fail(ErrExecution, safeFailure(err))
			}
			if _, err := tx.ExecContext(ctx, statement.SQL); err != nil {
				return fail(ErrExecution, safeFailure(err))
			}
		}
		if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
			return fail(err, "Permission was revoked before commit")
		}
		current, err := store.MigrationVersion(ctx, database)
		if err != nil {
			return fail(ErrExecution, "Website database connection was lost before commit")
		}
		if current != version {
			return fail(ErrStale, "Migration version changed before commit")
		}
		var appliedAt time.Time
		err = tx.QueryRowContext(ctx, `INSERT INTO _schakker.migration_receipts(namespace,attempt_id,version,sql_hash) VALUES($1,$2,$3,$4) RETURNING applied_at`, namespace, execution.ID, version+1, input.Review.SQLHash).Scan(&appliedAt)
		if err != nil {
			return fail(ErrExecution, safeFailure(err))
		}
		if err := tx.Commit(); err != nil {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			confirmed, exists, resolveErr := confirmCommit(cleanup, db, namespace, execution.ID)
			if resolveErr != nil {
				return ErrOutcomeUnknown
			}
			if !exists {
				return fail(ErrExecution, "Migration transaction did not commit")
			}
			if confirmed.SQLHash != input.Review.SQLHash || confirmed.Version != version+1 {
				return ErrOutcomeUnknown
			}
			appliedAt = confirmed.AppliedAt
		}
		appliedVersion := version + 1
		execution.Status = "succeeded"
		execution.AppliedVersion = &appliedVersion
		execution.FinishedAt = &appliedAt
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		if err := store.FinishExecution(cleanup, database, execution.ID, "succeeded", &appliedVersion, "", appliedAt); err != nil {
			return ErrHistoryPending
		}
		return nil
	})
	return execution, err
}

func confirmCommit(ctx context.Context, db *sql.DB, namespace string, id int64) (receipt, bool, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return receipt{}, false, err
	}
	defer tx.Rollback()
	if err := lockTarget(ctx, tx); err != nil {
		return receipt{}, false, err
	}
	return readReceipt(ctx, tx, namespace, id)
}

func safeFailure(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return fmt.Sprintf("PostgreSQL rejected the migration (SQLSTATE %s)", pg.Code)
	}
	if errors.Is(err, context.Canceled) {
		return "Migration was canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Migration exceeded its time limit"
	}
	return "Migration could not complete"
}
