package sqltext

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"github.com/wasilibs/go-pgquery/parser"
)

// ValidatePostgres uses PostgreSQL's grammar without connecting to a database or
// resolving objects. Dependent statements can therefore be validated together.
func ValidatePostgres(query string) error {
	_, err := parser.ParseToProtobuf(query)
	return err
}

// ForLog preserves SQL structure while removing comments and every literal.
// Malformed/unsupported input is never returned verbatim on scanner failure.
func ForLog(query string) string {
	if len(query) == 0 || len(query) > 2<<20 {
		return "[unavailable]"
	}
	scan, err := pgquery.Scan(query)
	if err != nil {
		return "[unavailable]"
	}
	var output strings.Builder
	for _, token := range scan.Tokens {
		if token.Token == pg.Token_SQL_COMMENT || token.Token == pg.Token_C_COMMENT {
			continue
		}
		if output.Len() > 0 {
			output.WriteByte(' ')
		}
		switch token.Token {
		case pg.Token_SCONST, pg.Token_USCONST, pg.Token_BCONST, pg.Token_XCONST,
			pg.Token_ICONST, pg.Token_FCONST, pg.Token_TRUE_P, pg.Token_FALSE_P, pg.Token_NULL_P:
			output.WriteByte('?')
		default:
			output.WriteString(query[token.Start:token.End])
		}
	}
	return output.String()
}
