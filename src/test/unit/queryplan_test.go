package unit_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"tubes1_wbd/backend/dbmanagement/adapters"
	mariaplan "tubes1_wbd/backend/dbmanagement/adapters/mariadb/queryplan"
	pgplan "tubes1_wbd/backend/dbmanagement/adapters/postgres/queryplan"
)

func planStatements(t *testing.T, query, dbms string) []adapters.Statement {
	t.Helper()
	statements, err := adapters.ParseSQL(query, dbms)
	if err != nil {
		t.Fatal(err)
	}
	return statements
}
func TestPostgreSQLQueryPlanParsesNativeTreeWithoutExecutingSQL(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Nested Loop","Join Type":"Inner","Plan Rows":4,"Startup Cost":0.2,"Total Cost":12.4,"Plans":[{"Node Type":"Index Scan","Relation Name":"people","Index Name":"people_pkey","Plan Rows":2},{"Node Type":"Seq Scan","Relation Name":"orders","Plan Rows":2}]}}]`
	db, _, _ := newSQLStub(t, execStep("SAVEPOINT schakker_explain"), scalarStep("EXPLAIN (FORMAT JSON) SELECT", raw), execStep("ROLLBACK TO SAVEPOINT"), execStep("RELEASE SAVEPOINT"))
	plans, err := pgplan.Analyze(context.Background(), db, planStatements(t, "SELECT * FROM people JOIN orders ON people.id=orders.person_id; ALTER TABLE people ADD COLUMN age integer", "postgresql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || !plans[0].Available || plans[1].Available || plans[1].Message == "" {
		t.Fatalf("incorrect plan availability: %#v", plans)
	}
	root := plans[0].Nodes[0]
	if root.Operation != "Nested Loop" || root.JoinType != "Inner" || len(root.Children) != 2 || root.Children[0].Index != "people_pkey" {
		t.Fatalf("native plan tree not preserved: %#v", root)
	}
	if root.EstimatedRows == nil || *root.EstimatedRows != 4 || root.TotalCost == nil || *root.TotalCost != 12.4 {
		t.Fatal("native estimates missing")
	}
	if string(plans[0].JSON) != raw {
		t.Fatal("raw native plan was changed")
	}
}
func TestPostgreSQLUnavailablePlanRecoversSavepoint(t *testing.T) {
	for _, step := range []sqlStep{{kind: "query", query: "EXPLAIN (FORMAT JSON)", err: errors.New("unknown column")}, scalarStep("EXPLAIN (FORMAT JSON)", "invalid JSON")} {
		db, _, _ := newSQLStub(t, execStep("SAVEPOINT"), step, execStep("ROLLBACK TO SAVEPOINT"), execStep("RELEASE SAVEPOINT"))
		plans, err := pgplan.Analyze(context.Background(), db, planStatements(t, "SELECT missing FROM people", "postgresql"))
		if err != nil {
			t.Fatal(err)
		}
		if plans[0].Available || !strings.Contains(plans[0].Message, "current schema") {
			t.Fatal("failed EXPLAIN fabricated a plan")
		}
	}
	db, _, _ := newSQLStub(t, sqlStep{kind: "exec", query: "SAVEPOINT", err: errors.New("savepoint unavailable")})
	_, err := pgplan.Analyze(context.Background(), db, planStatements(t, "SELECT 1", "postgresql"))
	wantError(t, err, adapters.ErrQuery)
}
func TestMariaDBQueryPlanParsesNativeJoinAndIndex(t *testing.T) {
	raw := `{"query_block":{"nested_loop":[{"table":{"table_name":"people","access_type":"ref","key":"people_name_idx","rows":3,"filtered":80}},{"block-nl-join":{"join_type":"BNL","table":{"table_name":"orders","access_type":"ALL","rows":10}}}]}}`
	step := queryStep("EXPLAIN FORMAT=JSON SELECT", []string{"plan"}, []driver.Value{[]byte(raw)})
	db, _, _ := newSQLStub(t, step)
	plans, err := mariaplan.Analyze(context.Background(), db, planStatements(t, "SELECT * FROM people JOIN orders ON people.id=orders.person_id; ALTER TABLE people ADD COLUMN age integer", "mariadb"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || !plans[0].Available || plans[1].Available {
		t.Fatalf("incorrect native availability: %#v", plans)
	}
	root := plans[0].Nodes[0]
	if root.Operation != "Nested Loop" || len(root.Children) != 2 || root.Children[0].Index != "people_name_idx" || root.Children[1].JoinType != "BNL" {
		t.Fatalf("native MariaDB plan not parsed: %#v", root)
	}
	if root.Children[0].FilteredPercent == nil || *root.Children[0].FilteredPercent != 80 {
		t.Fatal("native filtering estimate missing")
	}
}
func TestMariaDBUnavailableAndOptimizedPlans(t *testing.T) {
	db, _, _ := newSQLStub(t, sqlStep{kind: "query", query: "EXPLAIN FORMAT=JSON", err: errors.New("dependent schema change")})
	plans, err := mariaplan.Analyze(context.Background(), db, planStatements(t, "SELECT not_created_yet FROM people", "mariadb"))
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Available || plans[0].Message == "" {
		t.Fatal("unavailable MariaDB plan not reported")
	}
	db, _, _ = newSQLStub(t, scalarStep("EXPLAIN FORMAT=JSON", `{"query_block":{"message":"No tables used"}}`))
	plans, err = mariaplan.Analyze(context.Background(), db, planStatements(t, "SELECT 1", "mariadb"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans[0].Nodes) != 1 || plans[0].Nodes[0].Operation != "No tables used" {
		t.Fatal("optimized native message missing")
	}
}
