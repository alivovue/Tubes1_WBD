package queryplan

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func Analyze(ctx context.Context, q adapters.Querier, statements []adapters.Statement) ([]adapters.Plan, error) {
	result := make([]adapters.Plan, 0, len(statements))
	for _, statement := range statements {
		plan := adapters.Plan{Statement: statement.SQL, DBMS: "mariadb", Message: "Plan unavailable for this statement"}
		if statement.Explainable && statement.Command != "MERGE" {
			var raw []byte
			if err := q.QueryRowContext(ctx, `EXPLAIN FORMAT=JSON `+statement.SQL).Scan(&raw); err == nil && json.Valid(raw) {
				plan.Available = true
				plan.JSON = append([]byte(nil), raw...)
				plan.Message = ""
				var parsed any
				if json.Unmarshal(raw, &parsed) == nil {
					plan.Nodes = nodes(parsed)
				}
			} else {
				plan.Message = "Plan unavailable against the current schema; dependent changes may need execution first"
			}
		}
		result = append(result, plan)
	}
	return result, nil
}
func numeric(value any) *float64 {
	if number, ok := value.(float64); ok {
		return &number
	}
	return nil
}
func nodes(value any) []adapters.PlanNode {
	result := make([]adapters.PlanNode, 0)
	switch current := value.(type) {
	case map[string]any:
		if message, ok := current["message"].(string); ok {
			result = append(result, adapters.PlanNode{Operation: message})
		}
		if name, ok := current["table_name"].(string); ok {
			operation, _ := current["access_type"].(string)
			index, _ := current["key"].(string)
			node := adapters.PlanNode{Operation: operation, Relation: name, Index: index, EstimatedRows: numeric(current["rows"]), FilteredPercent: numeric(current["filtered"])}
			if costs, ok := current["cost_info"].(map[string]any); ok {
				node.TotalCost = numeric(costs["query_cost"])
			}
			result = append(result, node)
		}
		if children, ok := current["nested_loop"].([]any); ok {
			result = append(result, adapters.PlanNode{Operation: "Nested Loop", Children: nodes(children)})
		}
		if joined, ok := current["block-nl-join"].(map[string]any); ok {
			join, _ := joined["join_type"].(string)
			result = append(result, adapters.PlanNode{Operation: "Block Nested Loop", JoinType: join, Children: nodes(joined["table"])})
		}
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := current[key]
			if key == "nested_loop" || key == "block-nl-join" {
				continue
			}
			if key == "table_name" || key == "access_type" || key == "key" || strings.HasSuffix(key, "cost") {
				continue
			}
			result = append(result, nodes(child)...)
		}
	case []any:
		for _, child := range current {
			result = append(result, nodes(child)...)
		}
	}
	return result
}
