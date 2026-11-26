package response

import (
	"fmt"
	"net/http"

	"schakker_shared/httpapi"
)

func normalizeDisplayRows(rows [][]any) {
	for _, row := range rows {
		for i, value := range row {
			if bytes, ok := value.([]byte); ok {
				row[i] = string(bytes)
			} else if value != nil {
				row[i] = fmt.Sprint(value)
			}
		}
	}
}
func Write(w http.ResponseWriter, r *http.Request, data *Data) {
	if data.Review != nil && data.Review.SchemaDiff != nil {
		view := BuildSchemaDiffView(data.Review.SchemaDiff.Current, data.Review.SchemaDiff.Desired)
		data.SchemaDiff = &view
	}
	if data.Data != nil {
		normalizeDisplayRows(data.Data.Rows)
	}
	if data.ViewerResult != nil {
		normalizeDisplayRows(data.ViewerResult.Rows)
	}
	httpapi.JSON(w, r, http.StatusOK, data)
}
