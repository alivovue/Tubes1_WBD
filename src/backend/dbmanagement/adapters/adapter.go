package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tubes1_wbd/database/dao"
)

var (
	ErrInvalid     = dao.ErrInvalid
	ErrNotFound    = dao.ErrNotFound
	ErrConnection  = errors.New("target database connection is unavailable")
	ErrQuery       = errors.New("target database operation failed")
	ErrUnsupported = errors.New("operation is not supported by this database adapter")
)

type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Connection struct {
	Host           string
	Port           int
	DatabaseName   string
	Username       string
	Password       string
	SSLMode        string
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
}
type Schema struct {
	Name  string
	Owner string
}
type Table struct {
	Schema        string
	Name          string
	Kind          string
	EstimatedRows int64
	Engine        string
	Collation     string
	Definition    string
}
type Column struct {
	Name                string
	Position            int
	DataType            string
	Nullable            bool
	Default             *string
	Identity            string
	Generated           string
	GeneratedExpression *string
}
type Constraint struct {
	Name              string
	Type              string
	Definition        string
	Columns           []string
	ReferencedSchema  *string
	ReferencedTable   *string
	ReferencedColumns []string
	UpdateRule        string
	DeleteRule        string
	Expression        string
}
type IndexColumn struct {
	Name         string
	Expression   string
	Descending   bool
	PrefixLength int
}
type Index struct {
	Name            string
	Definition      string
	Method          string
	Unique          bool
	Primary         bool
	Valid           bool
	ConstraintOwned bool
	ConstraintName  string
	Columns         []IndexColumn
	Include         []string
	Predicate       string
}
type TableDetail struct {
	Table       Table
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}
type SortColumn struct {
	Name       string
	Descending bool
}
type PageOptions struct {
	Limit   int
	Offset  int
	OrderBy []SortColumn
}
type DataColumn struct {
	Name     string
	DataType string
}
type DataPage struct {
	Columns []DataColumn
	Rows    [][]any
	Limit   int
	Offset  int
	HasMore bool
}
type Snapshot struct {
	DBMS        string
	Schemas     []Schema
	Tables      []TableDetail
	Fingerprint string
}
type PlanNode struct {
	Operation       string
	Relation        string
	Index           string
	JoinType        string
	EstimatedRows   *float64
	StartupCost     *float64
	TotalCost       *float64
	FilteredPercent *float64
	Children        []PlanNode
}
type Plan struct {
	Statement string
	DBMS      string
	Available bool
	JSON      json.RawMessage
	Message   string
	Nodes     []PlanNode
}
type Token struct {
	Value   string
	Quoted  bool
	Literal bool
}
type Statement struct {
	HasComments bool
	SQL         string
	Command     string
	Explainable bool
	Tokens      []Token
}

type Adapter interface {
	Name() string
	DefaultPort() int
	Connect(context.Context, Connection) (*sql.DB, error)
	Schemas(context.Context, Querier) ([]Schema, error)
	Tables(context.Context, Querier, string) ([]Table, error)
	DescribeTable(context.Context, Querier, string, string) (TableDetail, error)
	TableData(context.Context, Querier, string, string, PageOptions) (DataPage, error)
	Fingerprint(context.Context, Querier) (string, error)
	Snapshot(context.Context, Querier) (Snapshot, error)
	QueryPlans(context.Context, Querier, []Statement) ([]Plan, error)
	QuoteIdentifier(...string) (string, error)
}

func Capture(ctx context.Context, q Querier, a Adapter) (Snapshot, error) {
	result := Snapshot{DBMS: a.Name(), Schemas: []Schema{}, Tables: []TableDetail{}}
	var err error
	result.Schemas, err = a.Schemas(ctx, q)
	if err != nil {
		return result, err
	}
	for _, schema := range result.Schemas {
		tables, err := a.Tables(ctx, q, schema.Name)
		if err != nil {
			return result, err
		}
		for _, table := range tables {
			detail, err := a.DescribeTable(ctx, q, schema.Name, table.Name)
			if err != nil {
				return result, err
			}
			result.Tables = append(result.Tables, detail)
		}
	}
	return result, nil
}
