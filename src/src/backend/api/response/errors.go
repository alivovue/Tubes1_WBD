package response

import (
	"errors"
	"net/http"

	"schakker_shared/httpapi"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
)

var ErrUnsavedDraft = errors.New("save the edited draft before analysis")

func Fail(w http.ResponseWriter, r *http.Request, err error) {
	message, status := ErrorStatus(err)
	httpapi.Error(w, r, message, status)
}
func ErrorStatus(err error) (string, int) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		return "You do not have permission for this action.", 403
	case errors.Is(err, access.ErrNotFound):
		return "The requested item was not found.", 404
	case errors.Is(err, migration.ErrStale), errors.Is(err, dbmanagement.ErrStaleSnapshot):
		return "The SQL, database or connection changed. Analyze the migration again.", 409
	case errors.Is(err, access.ErrConflict):
		return "This item changed or already exists. Reload and try again.", 409
	case errors.Is(err, ErrUnsavedDraft):
		return "This draft has unsaved SQL. Save the draft, then analyze the saved SQL before execution.", 409
	case errors.Is(err, migration.ErrNonAtomicAcceptance):
		return "Accept the non-atomic execution warning before continuing.", 400
	case errors.Is(err, migration.ErrOutcomeUnknown), errors.Is(err, migration.ErrHistoryPending):
		return "The execution outcome needs reconciliation. Check the changelog before retrying.", 409
	case errors.Is(err, migration.ErrExecution):
		return "Migration failed. Check its changelog entry for the outcome and completed statements.", 400
	case errors.Is(err, migration.ErrUnsafeSQL):
		return "Migration SQL cannot run procedural blocks, define executable routines, change session settings, use dynamic SQL helpers, or access internal migration metadata. Use explicit schema or data statements.", 400
	case errors.Is(err, diff.ErrDependency):
		return err.Error(), 409
	case errors.Is(err, diff.ErrNoChanges):
		return "The change set has no schema or data changes.", 400
	case errors.Is(err, dbmanagement.ErrReadOnly):
		return "SQL Viewer accepts one read-only SELECT query, including read-only CTEs and supported built-in functions. Writes, locking reads, output files and custom functions are not allowed.", 400
	case errors.Is(err, dbmanagement.ErrViewerResultSize):
		return "The result is too large. Select fewer columns, add a filter, or lower the LIMIT.", 400
	case errors.Is(err, dbmanagement.ErrViewerQuery):
		return "The read query failed. Check SQL syntax, table and column names, and database permissions, or simplify the query if it timed out.", 400
	case errors.Is(err, dbmanagement.ErrUnsupported):
		return "This operation is not supported for the selected database or schema.", 400
	case errors.Is(err, access.ErrInvalid):
		return "Check the supplied values and required fields.", 400
	case errors.Is(err, dbmanagement.ErrConnection):
		return "Cannot connect to the target database. Check its settings and availability.", 502
	case errors.Is(err, dbmanagement.ErrQuery):
		return "The database operation failed. Refresh the explorer and check the selected object.", 502
	default:
		return "The operation is temporarily unavailable. Please retry.", 500
	}
}
