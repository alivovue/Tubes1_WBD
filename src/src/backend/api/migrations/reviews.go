package migrations

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"schakker_shared/httpapi"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/migration"
)

func IsWorkflowRoute(path string) bool {
	return path == "/migration-definitions" || strings.HasPrefix(path, "/migration-definitions/") || path == "/migration-reviews" || strings.HasPrefix(path, "/migration-reviews/") || path == "/migrations/history" || path == "/migrations/activities" || path == "/migrations/state" || strings.HasSuffix(path, "/rollback-reviews") || strings.HasSuffix(path, "/forward-reviews") || strings.HasSuffix(path, "/cancel-reviews")
}
func decodeWorkflow(w http.ResponseWriter, r *http.Request, out any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return access.ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return access.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return access.ErrInvalid
	}
	return nil
}
func routeID(raw string) (int64, error) {
	id, err := httpapi.ID(raw)
	if err != nil || id < 1 {
		return 0, access.ErrInvalid
	}
	return id, nil
}
func pageInput(r *http.Request) (int64, int, int, error) {
	database, err := routeID(r.URL.Query().Get("database_id"))
	if err != nil {
		return 0, 0, 0, err
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, 0, access.ErrInvalid
		}
	}
	offset, err := httpapi.Offset(r.URL.Query().Get("offset"))
	return database, limit, offset, err
}
func (h Handler) ServeWorkflow(w http.ResponseWriter, r *http.Request, actor int64) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	workflow := h.Migrations.Workflow()
	var result any
	var err error
	status := 200
	switch {
	case len(parts) == 1 && parts[0] == "migration-definitions" && r.Method == "POST":
		var in struct {
			DatabaseID int64 `json:"database_id"`
			migration.DefinitionInput
		}
		if err = decodeWorkflow(w, r, &in); err == nil {
			result, err = workflow.CreateDefinition(r.Context(), actor, in.DatabaseID, in.DefinitionInput)
			status = 201
		}
	case len(parts) == 1 && parts[0] == "migration-definitions" && (r.Method == "GET" || r.Method == "HEAD"):
		var database int64
		var limit, offset int
		database, limit, offset, err = pageInput(r)
		if err == nil {
			result, err = workflow.Definitions(r.Context(), actor, database, limit, offset)
		}
	case len(parts) == 2 && parts[0] == "migration-definitions" && (r.Method == "GET" || r.Method == "HEAD"):
		var id int64
		id, err = routeID(parts[1])
		if err == nil {
			result, err = workflow.Definition(r.Context(), actor, id)
		}
	case len(parts) == 2 && parts[0] == "migration-definitions" && r.Method == "PATCH":
		var id int64
		id, err = routeID(parts[1])
		if err == nil {
			var in struct {
				ExpectedRevisionID int64 `json:"expected_revision_id"`
				migration.DefinitionInput
			}
			err = decodeWorkflow(w, r, &in)
			if err == nil {
				result, err = workflow.EditDefinition(r.Context(), actor, id, in.ExpectedRevisionID, in.DefinitionInput)
			}
		}
	case len(parts) == 1 && parts[0] == "migration-reviews" && r.Method == "POST":
		var in migration.ReviewInput
		if err = decodeWorkflow(w, r, &in); err == nil {
			result, err = workflow.SubmitReview(r.Context(), actor, in)
			status = 201
		}
	case len(parts) == 1 && parts[0] == "migration-reviews" && (r.Method == "GET" || r.Method == "HEAD"):
		var database int64
		var limit, offset int
		database, limit, offset, err = pageInput(r)
		if err == nil {
			result, err = workflow.Reviews(r.Context(), actor, database, limit, offset)
		}
	case len(parts) == 2 && parts[0] == "migration-reviews" && (r.Method == "GET" || r.Method == "HEAD"):
		var id int64
		id, err = routeID(parts[1])
		if err == nil {
			result, err = workflow.Review(r.Context(), actor, id)
		}
	case len(parts) == 3 && parts[0] == "migration-reviews" && parts[2] == "decisions" && r.Method == "POST":
		var id int64
		id, err = routeID(parts[1])
		if err == nil {
			var in struct {
				GroupID  int64  `json:"group_id"`
				Decision string `json:"decision"`
				Note     string `json:"note"`
			}
			err = decodeWorkflow(w, r, &in)
			if err == nil {
				result, err = workflow.Decide(r.Context(), actor, id, in.GroupID, in.Decision, in.Note)
			}
		}
	case len(parts) == 3 && parts[0] == "migration-reviews" && parts[2] == "enqueue" && r.Method == "POST":
		var id int64
		id, err = routeID(parts[1])
		if err == nil {
			var in struct {
				IdempotencyKey string `json:"idempotency_key"`
			}
			err = decodeWorkflow(w, r, &in)
			if err == nil {
				var job string
				job, err = workflow.Enqueue(r.Context(), actor, id, in.IdempotencyKey)
				result = map[string]any{"job_id": job, "status": "pending", "review_id": id}
				status = 202
			}
		}
	case len(parts) == 3 && parts[0] == "migrations" && (parts[2] == "rollback-reviews" || parts[2] == "forward-reviews") && r.Method == "POST":
		var in struct {
			ExpectedGeneration int64 `json:"expected_generation"`
		}
		if err = decodeWorkflow(w, r, &in); err == nil {
			if parts[2] == "rollback-reviews" {
				result, err = workflow.RollbackReview(r.Context(), actor, parts[1], in.ExpectedGeneration)
			} else {
				result, err = workflow.ForwardReview(r.Context(), actor, parts[1], in.ExpectedGeneration)
			}
			status = 201
		}
	case len(parts) == 3 && parts[0] == "migration-jobs" && parts[2] == "cancel-reviews" && r.Method == "POST":
		var in struct {
			RevisionID         int64 `json:"revision_id"`
			ExpectedGeneration int64 `json:"expected_generation"`
		}
		if err = decodeWorkflow(w, r, &in); err == nil {
			result, err = workflow.SubmitReview(r.Context(), actor, migration.ReviewInput{RevisionID: in.RevisionID, Action: "batch_cancel", TargetJobID: &parts[1], ExpectedGeneration: in.ExpectedGeneration})
			status = 201
		}
	case len(parts) == 2 && parts[0] == "migrations" && (parts[1] == "history" || parts[1] == "activities" || parts[1] == "state") && (r.Method == "GET" || r.Method == "HEAD"):
		var database int64
		var limit, offset int
		database, limit, offset, err = pageInput(r)
		if err == nil {
			switch parts[1] {
			case "state":
				result, err = workflow.State(r.Context(), actor, database)
			case "activities":
				result, err = workflow.Activities(r.Context(), actor, database, limit, offset)
			case "history":
				hide := false
				if raw := r.URL.Query().Get("hide_rollback"); raw != "" {
					hide, err = strconv.ParseBool(raw)
					if err != nil {
						err = access.ErrInvalid
					}
				}
				if err == nil {
					result, err = workflow.History(r.Context(), actor, database, hide, limit, offset)
				}
			}
		}
	default:
		w.Header().Set("Allow", "GET, HEAD, POST, PATCH")
		httpapi.Error(w, r, "Unsupported workflow endpoint or method", 405)
		return
	}
	if err != nil {
		response.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, r, status, result)
}
