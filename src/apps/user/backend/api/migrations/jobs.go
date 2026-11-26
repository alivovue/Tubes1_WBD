package migrations

import (
	"errors"
	"net/http"
	"strings"

	"schakker_shared/httpapi"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/database/dao"
)

type JobsHandler struct{ Jobs *queue.Service }

func IsJobRoute(path string) bool {
	return path == "/migration-jobs" || strings.HasPrefix(path, "/migration-jobs/") && !strings.HasSuffix(path, "/cancel-reviews")
}
func (h JobsHandler) Serve(w http.ResponseWriter, r *http.Request, actor int64) {
	if h.Jobs == nil {
		httpapi.Error(w, r, "Migration worker infrastructure is unavailable", 503)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 1 && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		database, limit, offset, err := pageInput(r)
		if err != nil || limit < 1 || limit > 200 {
			response.Fail(w, r, dao.ErrInvalid)
			return
		}
		view, err := h.Jobs.Workflow.Resources(r.Context(), actor, database)
		if err != nil {
			jobError(w, r, err)
			return
		}
		jobs, err := h.Jobs.Store.Jobs(r.Context(), database, limit, offset)
		if err != nil {
			jobError(w, r, err)
			return
		}
		items := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			p, err := h.Jobs.Store.BatchProgress(r.Context(), job.ID)
			if err != nil {
				jobError(w, r, err)
				return
			}
			item, err := view.Job(job, p)
			if err != nil {
				jobError(w, r, err)
				return
			}
			items = append(items, item)
		}
		httpapi.JSON(w, r, 200, map[string]any{"jobs": items})
		return
	}
	if len(parts) < 2 {
		httpapi.Error(w, r, "Job not found", 404)
		return
	}
	id := parts[1]
	var err error
	if len(parts) == 3 && parts[2] == "retry" && r.Method == http.MethodPost {
		var in struct{}
		if err = decodeWorkflow(w, r, &in); err == nil {
			_, err = h.Jobs.Retry(r.Context(), actor, id)
		}
	} else if len(parts) != 2 || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		w.Header().Set("Allow", "GET, HEAD, POST")
		httpapi.Error(w, r, "Unsupported job endpoint or method", 405)
		return
	}
	if err != nil {
		jobError(w, r, err)
		return
	}
	j, p, err := h.Jobs.Status(r.Context(), actor, id)
	if err != nil {
		jobError(w, r, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 202
	}
	view, err := h.Jobs.Workflow.Resources(r.Context(), actor, j.DatabaseID)
	if err != nil {
		jobError(w, r, err)
		return
	}
	item, err := view.Job(j, p)
	if err != nil {
		jobError(w, r, err)
		return
	}
	httpapi.JSON(w, r, status, map[string]any{"job": item})
}
func jobError(w http.ResponseWriter, r *http.Request, err error) {
	if queue.Conflict(err) || errors.Is(err, dao.ErrNotFound) {
		if errors.Is(err, dao.ErrNotFound) {
			httpapi.Error(w, r, "Migration job not found", 404)
		} else {
			httpapi.Error(w, r, "Migration job state changed", 409)
		}
		return
	}
	if errors.Is(err, migration.ErrWorkflowUnavailable) {
		httpapi.Error(w, r, "Migration authority unavailable", 503)
		return
	}
	response.Fail(w, r, err)
}
