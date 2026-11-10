package diff

import (
	"encoding/json"
	"errors"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

var ErrInvalid = adapters.ErrInvalid
var ErrUnsupported = adapters.ErrUnsupported
var ErrNoChanges = errors.New("schema and data change set is empty")

type Snapshot = adapters.Snapshot
type Change struct {
	Kind        string
	Schema      string
	Table       string
	Name        string
	Before      any
	After       any
	Destructive bool
}
type DataChange struct {
	Kind   string
	Schema string
	Table  string
	Values map[string]any
	Where  map[string]any
}
type Result struct {
	DBMS        string
	Changes     []Change
	SQL         string
	Statements  []string
	DataChanges []DataChange
	Destructive bool
	Current     Snapshot `json:"-"`
	Desired     Snapshot
}
type Operation struct {
	Kind       string
	Schema     string
	Table      string
	Column     adapters.Column
	Index      adapters.Index
	Definition *adapters.TableDetail
	Values     map[string]any
	Where      map[string]any
}

func clone(value Snapshot) (Snapshot, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Snapshot{}, err
	}
	var result Snapshot
	err = json.Unmarshal(raw, &result)
	return result, err
}
