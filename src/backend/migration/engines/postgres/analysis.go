package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type Engine struct{}

func New() Engine           { return Engine{} }
func (Engine) Name() string { return "postgresql" }
func (Engine) Parse(query string) ([]adapters.Statement, error) {
	statements, err := adapters.ParseSQL(query, "postgresql")
	if err != nil {
		return nil, err
	}
	for _, statement := range statements {
		switch statement.Command {
		case "CREATE", "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "SELECT", "WITH", "MERGE", "TRUNCATE", "COMMENT", "GRANT", "REVOKE", "REFRESH":
		default:
			return nil, engines.ErrUnsafeSQL
		}
		for i, t := range statement.Tokens {
			if t.Literal {
				continue
			}
			upper := strings.ToUpper(t.Value)
			if strings.EqualFold(t.Value, "_schakker") {
				return nil, engines.ErrUnsafeSQL
			}
			if upper == "SET_CONFIG" && i+1 < len(statement.Tokens) && adapters.IsWord(statement.Tokens[i+1], "(") || !t.Quoted && upper == "CONCURRENTLY" {
				return nil, engines.ErrUnsafeSQL
			}
			if !t.Quoted && upper == "U" && i+2 < len(statement.Tokens) && adapters.IsWord(statement.Tokens[i+1], "&") && statement.Tokens[i+2].Quoted {
				return nil, engines.ErrUnsafeSQL
			}
			if i+1 < len(statement.Tokens) && adapters.IsWord(statement.Tokens[i+1], "(") && (strings.HasPrefix(upper, "DBLINK") || upper == "QUERY_TO_XML" || upper == "QUERY_TO_XMLSCHEMA" || upper == "QUERY_TO_XML_AND_XMLSCHEMA") {
				return nil, engines.ErrUnsafeSQL
			}
		}
		if statement.Command == "CREATE" || statement.Command == "ALTER" || statement.Command == "DROP" {
			position := 1
			for position < len(statement.Tokens) {
				token := statement.Tokens[position]
				if token.Quoted || token.Literal {
					return nil, engines.ErrUnsafeSQL
				}
				upper := strings.ToUpper(token.Value)
				if upper != "OR" && upper != "REPLACE" && upper != "TEMP" && upper != "TEMPORARY" && upper != "UNLOGGED" && upper != "UNIQUE" && upper != "MATERIALIZED" && upper != "GLOBAL" && upper != "LOCAL" {
					break
				}
				position++
			}
			if position >= len(statement.Tokens) {
				return nil, engines.ErrUnsafeSQL
			}
			switch strings.ToUpper(statement.Tokens[position].Value) {
			case "TABLE", "INDEX", "VIEW", "SCHEMA", "SEQUENCE", "TYPE", "DOMAIN", "POLICY", "STATISTICS":
			default:
				return nil, engines.ErrUnsafeSQL
			}
		}
	}
	return statements, nil
}
func (Engine) Assess(context.Context, adapters.Querier, []adapters.Statement) (string, []string, error) {
	return engines.Transactional, nil, nil
}
func (Engine) SafeFailure(err error) string {
	if errors.Is(err, engines.ErrProtectedMetadata) {
		return "Protected migration metadata changed or could not be verified"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "2BP01" {
			return "Other objects depend on the object being changed. Remove those dependencies explicitly, or review a DROP ... CASCADE migration in the SQL editor"
		}
		return fmt.Sprintf("PostgreSQL rejected the migration (SQLSTATE %s)", pg.Code)
	}
	if errors.Is(err, context.Canceled) {
		return "Migration was canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Migration exceeded its time limit"
	}
	return "Migration could not complete"
}
