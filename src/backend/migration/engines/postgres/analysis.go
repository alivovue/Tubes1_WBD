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
		case "CREATE", "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "SELECT", "WITH", "MERGE", "TRUNCATE", "COMMENT", "GRANT", "REVOKE", "DO", "CALL", "REFRESH":
		default:
			return nil, adapters.ErrInvalid
		}
		for _, t := range statement.Tokens {
			if t.Literal {
				continue
			}
			upper := strings.ToUpper(t.Value)
			if strings.EqualFold(t.Value, "_schakker") {
				return nil, adapters.ErrInvalid
			}
			if !t.Quoted && (upper == "CONCURRENTLY" || upper == "SET_CONFIG") {
				return nil, adapters.ErrInvalid
			}
		}
		if (statement.Command == "CREATE" || statement.Command == "ALTER" || statement.Command == "DROP") && len(statement.Tokens) > 1 {
			switch strings.ToUpper(statement.Tokens[1].Value) {
			case "DATABASE", "TABLESPACE", "SYSTEM", "SUBSCRIPTION", "ROLE", "USER", "GROUP", "OWNED":
				return nil, adapters.ErrInvalid
			}
		}
	}
	return statements, nil
}
func (Engine) Assess(context.Context, adapters.Querier, []adapters.Statement) (string, []string, error) {
	return engines.Transactional, nil, nil
}
func (Engine) SafeFailure(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
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
