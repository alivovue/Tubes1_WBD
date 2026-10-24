package migration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/database/dao"
)

func namespaceCandidate() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func (s *Service) reconcileLocked(ctx context.Context, store *dao.Store, session engines.Session, database int64, namespace string) error {
	pending, err := store.UnfinishedExecutions(ctx, database)
	if err != nil {
		return err
	}
	for _, execution := range pending {
		receipt, err := session.Receipt(ctx, namespace, execution.ID)
		if err != nil {
			return ErrOutcomeUnknown
		}
		if receipt.Status == "prepared" {
			return ErrOutcomeUnknown
		}
		if receipt.Status == "committed" {
			if receipt.Version != execution.AttemptedVersion || receipt.SQLHash != SQLHash(execution.SQL) {
				return ErrHistoryPending
			}
			completed := receipt.CompletedStatements
			if receipt.TotalStatements == 0 {
				completed = execution.TotalStatements
			}
			if err := store.FinishExecution(ctx, database, execution.ID, "succeeded", &receipt.Version, "", receipt.AppliedAt, completed); err != nil {
				return ErrHistoryPending
			}
		} else {
			message := "Interrupted migration did not commit"
			if receipt.Mode == engines.NonAtomic {
				message = fmt.Sprintf("Interrupted non-atomic migration failed after %d/%d committed statements", receipt.CompletedStatements, receipt.TotalStatements)
			}
			if err := store.FinishExecution(ctx, database, execution.ID, "failed", nil, message, time.Now().UTC(), receipt.CompletedStatements); err != nil {
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
		target, err := s.databases.OpenMigrationTarget(ctx, actor, database)
		if err != nil {
			return err
		}
		defer target.DB.Close()
		engine, err := s.engines.Select(target.Adapter.Name())
		if err != nil {
			return err
		}
		session, err := engine.Open(ctx, target.DB, target.Adapter, engines.Reconcile)
		if err != nil {
			return err
		}
		defer session.Close()
		candidate, err := namespaceCandidate()
		if err != nil {
			return err
		}
		namespace, err := store.DatabaseNamespace(ctx, database, candidate)
		if err != nil {
			return err
		}
		return s.reconcileLocked(ctx, store, session, database, namespace)
	})
}
func (s *Service) ResolveOutcome(ctx context.Context, actor, database, id int64, applied bool, reason string) error {
	if err := s.access.RequireAdmin(ctx, actor); err != nil {
		return err
	}
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return ErrInvalid
	}
	return s.store.WithDatabaseLock(ctx, database, func(store *dao.Store) error {
		execution, err := store.Execution(ctx, database, id)
		if err != nil {
			return err
		}
		if execution.Status != "running" || execution.Mode != engines.NonAtomic {
			return ErrInvalid
		}
		target, err := s.databases.OpenMigrationTarget(ctx, actor, database)
		if err != nil {
			return err
		}
		defer target.DB.Close()
		engine, err := s.engines.Select(target.Adapter.Name())
		if err != nil {
			return err
		}
		session, err := engine.Open(ctx, target.DB, target.Adapter, engines.Reconcile)
		if err != nil {
			return err
		}
		defer session.Close()
		candidate, err := namespaceCandidate()
		if err != nil {
			return err
		}
		namespace, err := store.DatabaseNamespace(ctx, database, candidate)
		if err != nil {
			return err
		}
		receipt, err := session.Receipt(ctx, namespace, id)
		if err != nil {
			return err
		}
		if receipt.Status != "prepared" || receipt.SQLHash != SQLHash(execution.SQL) {
			return ErrInvalid
		}
		if err := store.RecordResolution(ctx, database, id, actor, reason); err != nil {
			return err
		}
		if err := session.Resolve(ctx, receipt, applied); err != nil {
			return err
		}
		return s.reconcileLocked(ctx, store, session, database, namespace)
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
	if input.Review.Mode == engines.NonAtomic && !input.AcceptNonAtomic {
		return dao.MigrationExecution{}, ErrNonAtomicAcceptance
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
		target, err := s.databases.OpenMigrationTarget(ctx, actor, database)
		if err != nil {
			return err
		}
		defer target.DB.Close()
		engine, err := s.engines.Select(target.Adapter.Name())
		if err != nil {
			return err
		}
		if engine.Name() != input.Review.DBMS {
			return ErrStale
		}
		mode, _, err := engine.Assess(ctx, target.DB, statements)
		if err != nil {
			return err
		}
		if mode != input.Review.Mode {
			return ErrStale
		}
		session, err := engine.Open(ctx, target.DB, target.Adapter, mode)
		if err != nil {
			return err
		}
		defer session.Close()
		candidate, err := namespaceCandidate()
		if err != nil {
			return err
		}
		namespace, err := store.DatabaseNamespace(ctx, database, candidate)
		if err != nil {
			return err
		}
		if err := s.reconcileLocked(ctx, store, session, database, namespace); err != nil {
			return err
		}
		version, err := store.MigrationVersion(ctx, database)
		if err != nil {
			return err
		}
		fingerprint, err := session.Fingerprint(ctx)
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
		execution, err = store.StartExecution(ctx, dao.MigrationExecution{DatabaseID: database, DraftID: draftID, ExecutedBy: actor, Description: strings.TrimSpace(input.Description), SQL: input.Review.SQL, AttemptedVersion: version + 1, Mode: mode, TotalStatements: len(statements)})
		if err != nil {
			return err
		}
		fail := func(cause error, message string) error {
			if !session.KnownFailure(cause) {
				return ErrOutcomeUnknown
			}
			session.Rollback()
			completed, total := session.Progress()
			if mode == engines.NonAtomic {
				message = fmt.Sprintf("Non-atomic migration failed after %d/%d committed statements: %s", completed, total, message)
			} else {
				message += "; transaction rolled back"
				completed = 0
			}
			finished := time.Now().UTC()
			execution.Status = "failed"
			execution.ErrorMessage = message
			execution.FinishedAt = &finished
			execution.CompletedStatements = completed
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			if err := store.FinishExecution(cleanup, database, execution.ID, "failed", nil, message, finished, completed); err != nil {
				return ErrHistoryPending
			}
			return cause
		}
		receipt := engines.Receipt{Namespace: namespace, AttemptID: execution.ID, Version: version + 1, SQLHash: input.Review.SQLHash, Mode: mode, TotalStatements: len(statements)}
		if err := session.Prepare(ctx, receipt); err != nil {
			return fail(ErrExecution, engine.SafeFailure(err))
		}
		for _, statement := range statements {
			if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
				return fail(err, "Permission was revoked")
			}
			if _, err := store.MigrationVersion(ctx, database); err != nil {
				return fail(err, "Website database connection was lost before execution")
			}
			if err := session.Execute(ctx, statement); err != nil {
				if !session.KnownFailure(err) {
					return ErrOutcomeUnknown
				}
				return fail(ErrExecution, engine.SafeFailure(err))
			}
		}
		if mode == engines.Transactional {
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
		}
		committed, err := session.Commit(ctx, receipt)
		if err != nil {
			session.Close()
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			confirmation, openErr := engine.Open(cleanup, target.DB, target.Adapter, engines.Reconcile)
			if openErr != nil {
				return ErrOutcomeUnknown
			}
			defer confirmation.Close()
			committed, openErr = confirmation.Receipt(cleanup, namespace, execution.ID)
			if openErr != nil || committed.Status == "prepared" {
				return ErrOutcomeUnknown
			}
			if committed.Status != "committed" {
				return fail(ErrExecution, "Migration did not commit")
			}
		}
		if committed.SQLHash != receipt.SQLHash || committed.Version != receipt.Version {
			return ErrOutcomeUnknown
		}
		appliedVersion := version + 1
		finished := committed.AppliedAt
		execution.Status = "succeeded"
		execution.AppliedVersion = &appliedVersion
		execution.FinishedAt = &finished
		execution.CompletedStatements = len(statements)
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		if err := store.FinishExecution(cleanup, database, execution.ID, "succeeded", &appliedVersion, "", finished, len(statements)); err != nil {
			return ErrHistoryPending
		}
		return nil
	})
	return execution, err
}
