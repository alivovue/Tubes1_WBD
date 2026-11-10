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
		entries, err := h.Migrations.History(r.Context(), data.User.ID, db.ID, 5, 0)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			data.Recent = append(data.Recent, response.RecentExecution{MigrationExecution: entry, DatabaseName: db.Name})
		}
	}
	sort.Slice(data.Recent, func(i, j int) bool { return data.Recent[i].StartedAt.After(data.Recent[j].StartedAt) })
	if len(data.Recent) > 8 {
		data.Recent = data.Recent[:8]
	}
	return nil
}
