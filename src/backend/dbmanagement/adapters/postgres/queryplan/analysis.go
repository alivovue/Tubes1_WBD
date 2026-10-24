package queryplan

import (
	"context"
	"encoding/json"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func Analyze(ctx context.Context, q adapters.Querier, statements []adapters.Statement) ([]adapters.Plan, error) {
	result := make([]adapters.Plan, 0, len(statements))
	for _, statement := range statements {
		plan := adapters.Plan{Statement: statement.SQL, DBMS: "postgresql", Message: "Plan unavailable for this statement"}
		if statement.Explainable {
			if _, err := q.ExecContext(ctx, `SAVEPOINT schakker_explain`); err != nil {
				return nil, adapters.ErrQuery
			}
			var raw []byte
			if err := q.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+statement.SQL).Scan(&raw); err == nil && json.Valid(raw) {
				plan.Available = true
				plan.JSON = append([]byte(nil), raw...)
				plan.Message = ""
				var roots []map[string]any
				if json.Unmarshal(raw, &roots) == nil {
					for _, root := range roots {
						if node, ok := root["Plan"].(map[string]any); ok {
							plan.Nodes = append(plan.Nodes, parseNode(node))
						}
					}
				}
			} else {
				plan.Message = "Plan unavailable against the current schema; dependent changes may need execution first"
			}
			if _, err := q.ExecContext(ctx, `ROLLBACK TO SAVEPOINT schakker_explain`); err != nil {
				return nil, adapters.ErrQuery
			}
			if _, err := q.ExecContext(ctx, `RELEASE SAVEPOINT schakker_explain`); err != nil {
				return nil, adapters.ErrQuery
			}
		}
		result = append(result, plan)
	}
	return result, nil
}
func number(m map[string]any, key string) *float64 {
	if v, ok := m[key].(float64); ok {
		return &v
	}
	return nil
}
func text(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func parseNode(m map[string]any) adapters.PlanNode {
	n := adapters.PlanNode{Operation: text(m, "Node Type"), Relation: text(m, "Relation Name"), Index: text(m, "Index Name"), JoinType: text(m, "Join Type"), EstimatedRows: number(m, "Plan Rows"), StartupCost: number(m, "Startup Cost"), TotalCost: number(m, "Total Cost")}
	children, _ := m["Plans"].([]any)
	for _, value := range children {
		if child, ok := value.(map[string]any); ok {
			n.Children = append(n.Children, parseNode(child))
		}
	}
	return n
}
