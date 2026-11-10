package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration"
)

func TestQueryPlanAPIReturnsNestedEstimatesForEachStatementWithoutExecution(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	var before string
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT sum(salary)::text FROM public.employees").Scan(&before))
	query := `SELECT e.name, (SELECT count(*) FROM public.employees c WHERE c.salary > e.salary) AS higher_paid FROM public.employees e ORDER BY e.name;
UPDATE public.employees SET salary=salary+100 RETURNING name;
ALTER TABLE public.employees ADD COLUMN plan_preview TEXT;
SELECT not_yet_created FROM public.employees;`
	response := apiRequest(t, userHandler(t, f), http.MethodPost, "/migrations/new", cookie, url.Values{
		"csrf": {csrf}, "db": {fmt.Sprint(target.ID)}, "action": {"analyze"}, "sql": {query}, "description": {"Preview plans only"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("plan analysis returned %d: %s", response.Code, response.Body.String())
	}
	result := decodeAPI[struct{ Review migration.Analysis }](t, response)
	plans := result.Review.Plans
	if len(plans) != 4 || !plans[0].Available || !plans[1].Available {
		t.Fatalf("statement plans missing: %#v", plans)
	}
	for index, prefix := range []string{"SELECT e.name", "UPDATE public.employees", "ALTER TABLE public.employees", "SELECT not_yet_created"} {
		if !strings.HasPrefix(plans[index].Statement, prefix) || plans[index].DBMS != "postgresql" {
			t.Fatalf("statement %d lost its association: %#v", index+1, plans[index])
		}
	}
	if len(plans[0].Nodes) != 1 || len(plans[0].Nodes[0].Children) == 0 {
		t.Fatal("API lost the nested plan hierarchy")
	}
	root := plans[0].Nodes[0]
	if root.Operation == "" || root.EstimatedRows == nil || root.StartupCost == nil || root.TotalCost == nil {
		t.Fatalf("API omitted native root estimates: %#v", root)
	}
	foundRelation := false
	var visit func([]adapters.PlanNode)
	visit = func(nodes []adapters.PlanNode) {
		for _, node := range nodes {
			if node.Relation == "employees" {
				foundRelation = true
			}
			visit(node.Children)
		}
	}
	visit(plans[0].Nodes)
	if !foundRelation {
		t.Fatal("API lost relation names in child nodes")
	}
	var native []struct{ Plan json.RawMessage }
	must(t, json.Unmarshal(plans[0].JSON, &native))
	if len(native) != 1 || len(native[0].Plan) == 0 {
		t.Fatal("original EXPLAIN JSON missing")
	}
	if plans[2].Available || plans[2].UnavailableReason != "unsupported_statement" || plans[3].Available || plans[3].UnavailableReason != "explain_unavailable" || plans[3].Message == "" {
		t.Fatal("unsupported and unavailable statement states were not preserved")
	}
	var after string
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT sum(salary)::text FROM public.employees").Scan(&after))
	if after != before {
		t.Fatal("plan analysis executed UPDATE")
	}
	table, err := f.databases.DescribeTable(f.ctx, f.editor, target.ID, "public", "employees")
	must(t, err)
	for _, column := range table.Columns {
		if column.Name == "plan_preview" {
			t.Fatal("plan analysis executed ALTER TABLE")
		}
	}
	history, err := f.migrations.History(f.ctx, f.editor, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("plan analysis created an execution attempt")
	}
}
