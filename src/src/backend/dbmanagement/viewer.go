package dbmanagement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
)

const ViewerRowLimit = 500

var ErrReadOnly = adapters.ErrReadOnly
var ErrViewerResultSize = errors.New("SQL Viewer result exceeds the response size limit")
var ErrViewerQuery = errors.New("SQL Viewer query failed")

var readNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type ReadResult struct {
	Columns  []DataColumn
	Rows     [][]any
	HasMore  bool
	Duration time.Duration
}

type ReadFilter struct {
	Column, Operator, Value, Type string
}

type ReadQuerySpec struct {
	Schema, Table string
	Columns       []string
	Filters       []ReadFilter
	OrderBy       []SortColumn
	Distinct      bool
	Limit, Offset int
}

func (s *Service) ReadQuery(ctx context.Context, actor, database int64, query string) (ReadResult, error) {
	started := time.Now()
	result := ReadResult{Rows: [][]any{}}
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	dbms, err := s.DBMS(ctx, actor, database)
	if err != nil {
		return result, err
	}
	statement, err := adapters.ValidateReadQuery(query, dbms)
	if err != nil {
		return result, err
	}
	target, err := s.open(ctx, actor, database, access.ViewOnly)
	if err != nil {
		return result, err
	}
	defer target.DB.Close()
	tx, err := target.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, ErrConnection
	}
	defer tx.Rollback()
	if dbms == "mariadb" {
		var name string
		if tx.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name) != nil {
			return result, ErrQuery
		}
		if err := adapters.ValidateMariaReferences(name, statement); err != nil {
			return result, err
		}
	}

	bounded := fmt.Sprintf("SELECT * FROM (\n%s\n) AS schakker_viewer_result LIMIT %d", statement.SQL, ViewerRowLimit+1)
	rows, err := tx.QueryContext(ctx, bounded)
	if err != nil {
		return result, ErrViewerQuery
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return result, ErrQuery
	}
	for _, column := range types {
		result.Columns = append(result.Columns, DataColumn{Name: column.Name(), DataType: column.DatabaseTypeName()})
	}
	bytesRead := 0
	for rows.Next() {
		if len(result.Rows) == ViewerRowLimit {
			result.HasMore = true
			break
		}
		values, dest := make([]any, len(types)), make([]any, len(types))
		for i := range values {
			dest[i] = &values[i]
		}
		if rows.Scan(dest...) != nil {
			return ReadResult{}, ErrQuery
		}
		for i, value := range values {
			switch v := value.(type) {
			case []byte:
				bytesRead += len(v)
				values[i] = append([]byte(nil), v...)
			case string:
				bytesRead += len(v)
			default:
				bytesRead += 16
			}
		}
		if bytesRead > 8<<20 {
			return ReadResult{}, ErrViewerResultSize
		}
		result.Rows = append(result.Rows, values)
	}
	if rows.Err() != nil {
		return ReadResult{}, ErrQuery
	}
	result.Duration = time.Since(started)
	return result, nil
}

func (s *Service) BuildReadQuery(ctx context.Context, actor, database int64, spec ReadQuerySpec) (string, error) {
	var query string
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		detail, err := a.DescribeTable(ctx, q, spec.Schema, spec.Table)
		if err != nil {
			return err
		}
		query, err = BuildReadSQL(a, detail, spec)
		return err
	})
	return query, err
}

func BuildReadSQL(a adapters.Adapter, detail TableDetail, spec ReadQuerySpec) (string, error) {
	if spec.Limit == 0 {
		spec.Limit = 100
	}
	if detail.Table.Schema != spec.Schema || detail.Table.Name != spec.Table || spec.Limit < 1 || spec.Limit > ViewerRowLimit || spec.Offset < 0 || spec.Offset > 1_000_000 || len(spec.Filters) > 20 || len(spec.OrderBy) > 10 {
		return "", ErrInvalid
	}
	columns := map[string]bool{}
	for _, column := range detail.Columns {
		columns[column.Name] = true
	}
	quoteColumn := func(name string) (string, error) {
		if !columns[name] {
			return "", ErrInvalid
		}
		return a.QuoteIdentifier(name)
	}
	table, err := a.QuoteIdentifier(spec.Schema, spec.Table)
	if err != nil {
		return "", err
	}
	projection := []string{}
	for _, name := range spec.Columns {
		column, err := quoteColumn(name)
		if err != nil {
			return "", err
		}
		projection = append(projection, column)
	}
	if len(projection) == 0 {
		projection = []string{"*"}
	}
	distinct := ""
	if spec.Distinct {
		distinct = "DISTINCT "
	}
	query := "SELECT " + distinct + strings.Join(projection, ", ") + "\nFROM " + table
	conditions := []string{}
	for _, filter := range spec.Filters {
		column, err := quoteColumn(filter.Column)
		if err != nil {
			return "", err
		}
		op := strings.ToUpper(strings.TrimSpace(filter.Operator))
		switch op {
		case "IS NULL", "IS NOT NULL":
			conditions = append(conditions, column+" "+op)
		case "=", "!=", "<>", "<", "<=", ">", ">=", "LIKE", "NOT LIKE":
			value, err := readFilterLiteral(filter)
			if err != nil {
				return "", err
			}
			conditions = append(conditions, column+" "+op+" "+value)
		default:
			return "", ErrInvalid
		}
	}
	if len(conditions) > 0 {
		query += "\nWHERE " + strings.Join(conditions, " AND ")
	}
	order := []string{}
	for _, sort := range spec.OrderBy {
		column, err := quoteColumn(sort.Name)
		if err != nil {
			return "", err
		}
		direction := " ASC"
		if sort.Descending {
			direction = " DESC"
		}
		order = append(order, column+direction)
	}
	if len(order) > 0 {
		query += "\nORDER BY " + strings.Join(order, ", ")
	}
	query += fmt.Sprintf("\nLIMIT %d OFFSET %d;", spec.Limit, spec.Offset)
	if _, err := adapters.ValidateReadQuery(query, a.Name()); err != nil {
		return "", err
	}
	return query, nil
}

func readFilterLiteral(filter ReadFilter) (string, error) {
	if len(filter.Value) > 8192 || strings.ContainsRune(filter.Value, 0) {
		return "", ErrInvalid
	}
	switch filter.Type {
	case "", "text":
		return "'" + strings.ReplaceAll(filter.Value, "'", "''") + "'", nil
	case "number":
		if !readNumber.MatchString(filter.Value) {
			return "", ErrInvalid
		}

		return filter.Value, nil
	case "boolean":
		if filter.Value == "true" {
			return "TRUE", nil
		}
		if filter.Value == "false" {
			return "FALSE", nil
		}
	}
	return "", ErrInvalid
}
