package observability

import "context"

type migrationKey struct{}

type MigrationIdentity struct {
	DefinitionID, RevisionID, Revision, ReviewID int64
	JobID, ExecutionID                           string
	Attempt                                      int
	Batch                                        int64
}

func WithMigration(ctx context.Context, identity MigrationIdentity) context.Context {
	return context.WithValue(ctx, migrationKey{}, identity)
}

func WithBatch(ctx context.Context, batch int64) context.Context {
	identity, _ := ctx.Value(migrationKey{}).(MigrationIdentity)
	identity.Batch = batch
	return WithMigration(ctx, identity)
}

func migrationFields(ctx context.Context) []any {
	identity, ok := ctx.Value(migrationKey{}).(MigrationIdentity)
	if !ok {
		return nil
	}
	fields := []any{"job_id", identity.JobID, "execution_id", identity.ExecutionID, "attempt", identity.Attempt}
	if identity.DefinitionID > 0 {
		fields = append(fields, "definition_id", identity.DefinitionID, "migration_id", identity.DefinitionID,
			"revision_id", identity.RevisionID, "revision", identity.Revision, "review_id", identity.ReviewID)
	}
	if identity.Batch > 0 {
		fields = append(fields, "batch_no", identity.Batch)
	}
	return fields
}
