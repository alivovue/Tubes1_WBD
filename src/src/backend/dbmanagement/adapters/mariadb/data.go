package mariadb

import (
	"bytes"
	"context"
	"strings"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func (a Adapter) TableData(ctx context.Context, q adapters.Querier, schema, table string, options adapters.PageOptions) (adapters.DataPage, error) {
	page := adapters.DataPage{Columns: []adapters.DataColumn{}, Rows: [][]any{}}
	if options.Limit < 0 || options.Limit > 500 || options.Offset < 0 || len(options.OrderBy) > 16 {
		return page, adapters.ErrInvalid
	}
	if options.Limit == 0 {
		options.Limit = 100
	}
	page.Limit = options.Limit
	page.Offset = options.Offset
	detail, err := a.DescribeTable(ctx, q, schema, table)
	if err != nil {
		return page, err
	}
	known := make(map[string]bool)
	for _, column := range detail.Columns {
		known[column.Name] = true
	}
	order := make([]string, 0)
	for _, sort := range options.OrderBy {
		if !known[sort.Name] {
			return page, adapters.ErrInvalid
		}
		column, _ := a.QuoteIdentifier(sort.Name)
		if sort.Descending {
			column += " DESC"
		}
		order = append(order, column)
	}
	if len(order) == 0 {
		for _, constraint := range detail.Constraints {
			if constraint.Type == "p" {
				for _, column := range constraint.Columns {
					name, _ := a.QuoteIdentifier(column)
					order = append(order, name)
				}
				break
			}
		}
	}
	name, err := a.QuoteIdentifier(schema, table)
	if err != nil {
		return page, err
	}
	query := "SELECT * FROM " + name
	if len(order) > 0 {
		query += " ORDER BY " + strings.Join(order, ",")
	}
	rows, err := q.QueryContext(ctx, query+` LIMIT ? OFFSET ?`, options.Limit+1, options.Offset)
	if err != nil {
		return page, adapters.ErrQuery
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return page, adapters.ErrQuery
	}
	for _, column := range types {
		page.Columns = append(page.Columns, adapters.DataColumn{Name: column.Name(), DataType: column.DatabaseTypeName()})
	}
	for rows.Next() {
		values := make([]any, len(types))
		dest := make([]any, len(types))
		for i := range values {
			dest[i] = &values[i]
		}
		if rows.Scan(dest...) != nil {
			return page, adapters.ErrQuery
		}
		if len(page.Rows) == options.Limit {
			page.HasMore = true
			break
		}
		for i, value := range values {
			if data, ok := value.([]byte); ok {
				values[i] = bytes.Clone(data)
			}
		}
		page.Rows = append(page.Rows, values)
	}
	if rows.Err() != nil {
		return page, adapters.ErrQuery
	}
	return page, nil
}
