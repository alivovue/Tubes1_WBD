package worker

import (
	"context"

	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
	"tubes1_wbd/database/dao"
)

type Recovery struct {
	Store     *dao.Store
	Databases *dbmanagement.Service
}

func (r Recovery) CancellationSource(ctx context.Context, id string) (migration.RecoverySource, error) {
	return r.CancellationSourceFor(ctx, 0, id)
}
func (r Recovery) CancellationSourceFor(ctx context.Context, actor int64, id string) (migration.RecoverySource, error) {
	j, err := r.Store.Job(ctx, id)
	if err != nil {
		return migration.RecoverySource{}, err
	}
	source := migration.RecoverySource{JobID: j.ID, DatabaseID: j.DatabaseID, Paused: j.LeaseOwner == "" && (j.Status == "partial" || j.Status == "uncertain" || j.Status == "partially_reverted")}
	if !source.Paused {
		return source, nil
	}
	review, err := r.Store.Review(ctx, j.ReviewID, false)
	if err != nil {
		return source, err
	}
	source.RevisionID = review.RevisionID
	revision, err := r.Store.Revision(ctx, review.RevisionID)
	if err != nil {
		return source, err
	}
	batch, err := engines.ParseBatch(revision.UpSQL, revision.DownSQL)
	if err != nil {
		return source, err
	}
	if batch == nil {
		return source, nil
	}
	if actor < 1 {
		actor = j.ActorID
	}
	target, err := r.Databases.OpenMigrationTarget(ctx, actor, j.DatabaseID)
	if err != nil {
		return source, err
	}
	defer target.DB.Close()
	if target.Adapter.Name() != "postgresql" {
		return source, dbmanagement.ErrUnsupported
	}
	namespace, err := r.Store.ExistingDatabaseNamespace(ctx, j.DatabaseID)
	if err != nil {
		return source, err
	}
	progress, fingerprint, safe, err := pgengine.BatchRecovery(ctx, target.DB, target.Adapter, namespace, id)
	if err != nil {
		return source, err
	}
	source.Fingerprint = fingerprint
	source.PartiallyCommitted = progress.Batches > 0
	source.SafeCompensation = safe
	return source, nil
}

var _ migration.RecoveryProvider = Recovery{}
