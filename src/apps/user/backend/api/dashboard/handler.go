package dashboard

import (
	"net/http"
	"sort"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/api/selection"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
)

type Handler struct {
	Databases  *dbmanagement.Service
	Migrations *migration.Service
}

func (h Handler) Load(r *http.Request, data *response.Data) error {
	if err := selection.List(r, data, h.Databases); err != nil {
		return err
	}

	for _, db := range data.Databases {
		if db.Permission == access.ViewEdit {
			data.EditableCount++
		}
		workflow := h.Migrations.Workflow()
		entries, err := workflow.History(r.Context(), data.User.ID, db.ID, false, 8, 0)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			continue
		}
		view, err := workflow.Resources(r.Context(), data.User.ID, db.ID)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			summary, err := view.ExecutionSummary(entry)
			if err != nil {
				return err
			}
			data.Recent = append(data.Recent, response.RecentExecution{ExecutionSummary: summary, DatabaseName: db.Name})
		}
	}
	sort.SliceStable(data.Recent, func(i, j int) bool { return data.Recent[i].CommittedAt.After(data.Recent[j].CommittedAt) })
	if len(data.Recent) > 8 {
		data.Recent = data.Recent[:8]
	}
	return nil
}
