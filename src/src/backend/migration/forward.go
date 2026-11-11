package migration

import (
	"context"
	"tubes1_wbd/database/dao"
)

func (w *Workflow) ForwardReview(ctx context.Context, actor int64, execution string, generation int64) (dao.MigrationReview, error) {
	if !uuidPattern.MatchString(execution) {
		return dao.MigrationReview{}, ErrInvalid
	}
	e, err := w.store.CommittedExecution(ctx, execution)
	if err != nil {
		return dao.MigrationReview{}, err
	}
	return w.SubmitReview(ctx, actor, ReviewInput{RevisionID: e.RevisionID, Action: "forward", TargetExecutionID: &execution, ExpectedGeneration: generation})
}
