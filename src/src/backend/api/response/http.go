package response

import (
	"fmt"
	"net/http"
	"net/url"

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
func link(route string, args ...any) string {
	values := url.Values{}
	for i := 0; i+1 < len(args); i += 2 {
		values.Set(fmt.Sprint(args[i]), fmt.Sprint(args[i+1]))
	}
	if len(values) == 0 {
		return route
	}
	return route + "?" + values.Encode()
}
func Redirect(w http.ResponseWriter, r *http.Request, route, message string, args ...any) {
	args = append(args, "notice", message)
	httpapi.JSON(w, r, http.StatusOK, map[string]string{"location": link(route, args...)})
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
