package integration_test

import (
	"fmt"
	"testing"

	"tubes1_wbd/backend/migration"
)

func TestExecutionRecordsTheEditorWhoEnqueuedIt(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	executor := f.user(t, "execution.operator")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, executor.ID, f.editors.ID, true))
	for _, failed := range []bool{false, true} {
		input := migration.DefinitionInput{Title: "Execution attribution", UpSQL: "SELECT 1", DownSQL: "SELECT 1"}
		if failed {
			input.UpSQL = "SELECT 1/0"
		}
		revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, input)
		must(t, err)
		review := f.approve(t, f.submit(t, revision))
		id, err := f.workflow.Enqueue(f.ctx, executor.ID, review.ID, fmt.Sprintf("attribution-%d", review.ID))
		must(t, err)
		_, err = runner.RunOnce(f.ctx)
		status := "succeeded"
		if failed {
			status = "failed"
			if err == nil {
				t.Fatal("division by zero succeeded")
			}
		} else {
			must(t, err)
		}
		job := assertJob(t, f, id, status)
		if job.ActorID != executor.ID {
			t.Fatalf("job actor=%d, want executor %d", job.ActorID, executor.ID)
		}
		events, err := f.workflow.ExecutionHistory(f.ctx, executor.ID, f.database, false, 50, 0)
		must(t, err)
		if len(events) == 0 || events[0].ActorID != executor.ID {
			t.Fatal("attempt history lost the actual executor")
		}
		if !failed {
			history, err := f.workflow.History(f.ctx, executor.ID, f.database, false, 50, 0)
			must(t, err)
			if len(history) != 1 || history[0].ActorID != executor.ID {
				t.Fatal("committed history lost the actual executor")
			}
		}
		saved, err := f.workflow.Review(f.ctx, executor.ID, review.ID)
		must(t, err)
		if saved.SubmittedBy != f.editor {
			t.Fatal("executing a job replaced the review's submitter")
		}
	}
}
