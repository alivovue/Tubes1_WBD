package sqltext

import (
	"encoding/json"
	"sort"

	pgquery "github.com/wasilibs/go-pgquery"
)

// Relation is a name in PostgreSQL's parse tree, before search-path resolution.
type Relation struct{ Schema, Name string }

// PostgresRelations identifies relation references for snapshot scoping. Function
// calls, operators, casts and unknown statement kinds conservatively require all
// tracked relations: their runtime effects cannot be inferred from SQL names.
func PostgresRelations(query string) ([]Relation, bool, error) {
	raw, err := pgquery.ParseToJSON(query)
	if err != nil {
		return nil, false, err
	}
	var tree any
	if err = json.Unmarshal([]byte(raw), &tree); err != nil {
		return nil, false, err
	}
	found := map[Relation]bool{}
	dynamic := false
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			// Typed fields such as CreateStmt.relation and ColumnDef.typeName
			// are not wrapped in a RangeVar/TypeName Node in PostgreSQL JSON.
			if table, ok := v["relname"].(string); ok && table != "" {
				schema, _ := v["schemaname"].(string)
				found[Relation{schema, table}] = true
			}
			for key, item := range v {
				switch key {
				case "stmt":
					if stmt, ok := item.(map[string]any); ok {
						for kind := range stmt {
							switch kind {
							case "CreateStmt", "AlterTableStmt", "UpdateStmt", "InsertStmt", "DeleteStmt", "SelectStmt", "IndexStmt", "ViewStmt", "TruncateStmt":
							default:
								dynamic = true
							}
						}
					}
				case "RangeVar":
					if name, ok := item.(map[string]any); ok {
						schema, _ := name["schemaname"].(string)
						table, _ := name["relname"].(string)
						if table != "" {
							found[Relation{schema, table}] = true
						}
					}
				case "TypeName", "typeName":
					// Built-in type aliases normalize to pg_catalog names. A
					// user type can execute custom input/domain routines.
					node, ok := item.(map[string]any)
					if !ok {
						dynamic = true
						break
					}
					names, _ := node["names"].([]any)
					builtin := false
					if len(names) == 2 {
						if part, ok := names[0].(map[string]any); ok {
							if str, ok := part["String"].(map[string]any); ok {
								builtin = str["sval"] == "pg_catalog"
							}
						}
					}
					if !builtin {
						dynamic = true
					}
				case "FuncCall", "A_Expr", "TypeCast", "SubLink":
					dynamic = true
				}
				walk(item)
			}
		}
	}
	walk(tree)
	out := make([]Relation, 0, len(found))
	for relation := range found {
		out = append(out, relation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema == out[j].Schema {
			return out[i].Name < out[j].Name
		}
		return out[i].Schema < out[j].Schema
	})
	return out, dynamic, nil
}
