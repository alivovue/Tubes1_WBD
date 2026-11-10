package mariadb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type Engine struct{}

func New() Engine           { return Engine{} }
func (Engine) Name() string { return "mariadb" }
func (Engine) Parse(query string) ([]adapters.Statement, error) {
	statements, err := adapters.ParseSQL(query, "mariadb")
	if err != nil {
		return nil, err
	}
	for _, statement := range statements {
		if statement.Command == "RENAME" && (len(statement.Tokens) < 2 || !adapters.IsWord(statement.Tokens[1], "TABLE")) {
			return nil, adapters.ErrUnsupported
		}
		switch statement.Command {
		case "CREATE", "ALTER", "DROP", "RENAME", "TRUNCATE", "INSERT", "UPDATE", "DELETE", "SELECT", "WITH", "REPLACE":
		default:
			return nil, adapters.ErrInvalid
		}
		if statement.Command == "CREATE" || statement.Command == "ALTER" || statement.Command == "DROP" {
			if len(statement.Tokens) < 2 {
				return nil, adapters.ErrInvalid
			}
			kind := strings.ToUpper(statement.Tokens[1].Value)
			if kind == "OR" && len(statement.Tokens) > 3 && adapters.IsWord(statement.Tokens[2], "REPLACE") {
				kind = strings.ToUpper(statement.Tokens[3].Value)
			}
			switch kind {
			case "TABLE", "INDEX", "UNIQUE", "FULLTEXT", "SPATIAL", "VIEW", "FUNCTION", "PROCEDURE", "TRIGGER", "SEQUENCE":
			default:
				return nil, adapters.ErrUnsupported
			}
		}
		for i, t := range statement.Tokens {
			if !t.Literal && i+1 < len(statement.Tokens) && statement.Tokens[i+1].Value == "(" {
				switch strings.ToUpper(t.Value) {
				case "GET_LOCK", "RELEASE_LOCK", "RELEASE_ALL_LOCKS", "LOAD_FILE":
					return nil, adapters.ErrInvalid
				}
			}
			if t.Literal {
				continue
			}
			upper := strings.ToUpper(t.Value)
			if strings.HasPrefix(strings.ToLower(t.Value), "_schakker_") {
				return nil, adapters.ErrInvalid
			}
			if !t.Quoted {
				switch upper {
				case "OUTFILE", "DUMPFILE", "GET_LOCK", "RELEASE_LOCK", "RELEASE_ALL_LOCKS", "AUTOCOMMIT", "INFILE", "LOAD_FILE":
					return nil, adapters.ErrInvalid
				}
			}
		}
	}
	return statements, nil
}

func (Engine) Assess(ctx context.Context, q adapters.Querier, statements []adapters.Statement) (string, []string, error) {
	var database string
	if q.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&database) != nil {
		return "", nil, adapters.ErrQuery
	}
	nonAtomic := false
	for _, statement := range statements {
		if err := adapters.ValidateMariaReferences(database, statement); err != nil {
			return "", nil, err
		}
		switch statement.Command {
		case "CREATE", "ALTER", "DROP", "RENAME", "TRUNCATE":
			nonAtomic = true
		}
	}
	var unsafe int
	if !nonAtomic {
		if q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE<>'VIEW' AND ENGINE<>'InnoDB' AND LEFT(TABLE_NAME,10)<>'_schakker_')+(SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE())+(SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE() AND ROUTINE_TYPE='FUNCTION')`).Scan(&unsafe) != nil {
			return "", nil, adapters.ErrQuery
		}
		nonAtomic = unsafe > 0
	}
	if nonAtomic {
		return engines.NonAtomic, []string{"MariaDB executes this batch without a transaction. Each successful statement commits independently; a failure may leave partial changes. Explicit acceptance is required."}, nil
	}
	return engines.Transactional, nil, nil
}

func (Engine) SafeFailure(err error) string {
	var native *mysql.MySQLError
	if errors.As(err, &native) {
		switch native.Number {
		case 1075:
			return "An AUTO_INCREMENT column must remain indexed. Keep or replace its key in the same ALTER TABLE statement"
		case 1553, 1828, 1833:
			return "A foreign key depends on this column or index. Remove or change the foreign key explicitly before retrying"
		}
		return fmt.Sprintf("MariaDB rejected the statement (error %d, SQLSTATE %s)", native.Number, native.SQLState[:])
	}
	if errors.Is(err, context.Canceled) {
		return "Migration was canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Migration exceeded its time limit"
	}
	return "Migration could not complete"
}
