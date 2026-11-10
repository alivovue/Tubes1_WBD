package postgres

import (
	"bytes"
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

func (s Adapter) TableData(ctx context.Context, q adapters.Querier, schema, table string, options PageOptions) (DataPage, error) {
	if !validSchema(schema) || !validIdentifier(table) || options.Offset < 0 || options.Limit < 0 || options.Limit > 500 || len(options.OrderBy) > 16 {
		return DataPage{}, ErrInvalid
	}
	if options.Limit == 0 {
		options.Limit = 100
	}
	page := DataPage{Columns: []DataColumn{}, Rows: [][]any{}, Limit: options.Limit, Offset: options.Offset}
	err := s.read(ctx, q, func(ctx context.Context, tx adapters.Querier) error {
		detail, err := describe(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		known := make(map[string]bool)
		for _, column := range detail.Columns {
			known[column.Name] = true
		}
		order := make([]string, 0, len(options.OrderBy))
		for _, sort := range options.OrderBy {
			if !known[sort.Name] {
				return ErrInvalid
			}
			part := pgx.Identifier{sort.Name}.Sanitize()
			if sort.Descending {
				part += " DESC"
			}
			order = append(order, part)
		}
		if len(order) == 0 {
			for _, constraint := range detail.Constraints {
				if constraint.Type == "p" {
					for _, column := range constraint.Columns {
						order = append(order, pgx.Identifier{column}.Sanitize())
					}
					break
				}
			}
		}
		query := `SELECT * FROM ` + pgx.Identifier{schema, table}.Sanitize()
		if len(order) > 0 {
			query += " ORDER BY " + strings.Join(order, ",")
		}
		rows, err := tx.QueryContext(ctx, query+` LIMIT $1 OFFSET $2`, options.Limit+1, options.Offset)
		if err != nil {
			return ErrQuery
		}
		defer rows.Close()
		types, err := rows.ColumnTypes()
		if err != nil {
			return ErrQuery
		}
		for _, column := range types {
			page.Columns = append(page.Columns, DataColumn{Name: column.Name(), DataType: column.DatabaseTypeName()})
		}
		for rows.Next() {
			values := make([]any, len(types))
			dest := make([]any, len(types))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return ErrQuery
			}
			if len(page.Rows) == options.Limit {
				page.HasMore = true
				break
			}
			for i, value := range values {
				if b, ok := value.([]byte); ok {
					values[i] = bytes.Clone(b)
				}
			}
			page.Rows = append(page.Rows, values)
		}
		if rows.Err() != nil {
			return ErrQuery
		}
		return nil
	})
	return page, err
}
