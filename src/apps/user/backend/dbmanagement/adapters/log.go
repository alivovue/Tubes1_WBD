package adapters

import (
	"regexp"
	"strings"

	"schakker_shared/sqltext"
)

var logNumericSuffix = regexp.MustCompile(`(?i)^(?:e[0-9]*|x[0-9a-f]+|b[01]+)$`)

// SQLLogText uses the same MariaDB quoting rules as query validation. In
// particular, double-quoted strings must not be logged as PostgreSQL identifiers.
func SQLLogText(query, dbms string) string {
	if Normalize(dbms) != "mariadb" {
		return sqltext.ForLog(query)
	}
	// Ambiguous backslash escaping is safer to omit than to interpret differently
	// from the target server's SQL mode.
	if strings.ContainsRune(query, '\\') {
		return "[unavailable]"
	}
	statements, err := ParseSQL(query, dbms)
	if err != nil {
		return "[unavailable]"
	}
	var parts []string
	number := false
	for i, statement := range statements {
		if i > 0 {
			parts = append(parts, ";")
		}
		for _, token := range statement.Tokens {
			value := token.Value
			switch {
			case token.Literal:
				value = "?"
				number = false
			case token.Quoted:
				value = "`" + strings.ReplaceAll(value, "`", "``") + "`"
				number = false
			case len(value) > 0 && value[0] >= '0' && value[0] <= '9', number && logNumericSuffix.MatchString(value):
				value = "?"
				number = true
			case strings.EqualFold(value, "TRUE"), strings.EqualFold(value, "FALSE"), strings.EqualFold(value, "NULL"):
				value = "?"
				number = false
			default:
				if value != "." && value != "+" && value != "-" {
					number = false
				}
			}
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}
