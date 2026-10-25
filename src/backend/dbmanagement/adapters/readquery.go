package adapters

import (
	"errors"
	"strings"
)

var ErrReadOnly = errors.New("SQL Viewer accepts one read-only SELECT query and supported built-in functions")

func ValidateReadQuery(query, dbms string) (Statement, error) {
	statements, err := ParseSQL(query, dbms)
	if err != nil || len(statements) != 1 {
		return Statement{}, ErrReadOnly
	}
	s := statements[0]
	if s.Command != "SELECT" && s.Command != "WITH" {
		return Statement{}, ErrReadOnly
	}
	for i, token := range s.Tokens {
		if token.Literal {
			if i+1 < len(s.Tokens) && IsWord(s.Tokens[i+1], "(") {
				return Statement{}, ErrReadOnly
			}
			continue
		}
		if strings.EqualFold(token.Value, "_schakker") {
			return Statement{}, ErrReadOnly
		}
		if !token.Quoted {
			switch strings.ToUpper(token.Value) {
			case "INSERT", "UPDATE", "DELETE", "MERGE", "CREATE", "ALTER", "DROP", "TRUNCATE", "INTO", "COPY", "CALL", "DO", "EXECUTE", "PREPARE", "DEALLOCATE", "SET", "RESET", "BEGIN", "START", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "GRANT", "REVOKE", "LOCK", "FOR", "OUTFILE", "DUMPFILE", "LOAD", "HANDLER", "ANALYZE", "EXPLAIN", "VACUUM", "REFRESH", "ATTACH", "DETACH", "PRAGMA", "@", ":=":
				return Statement{}, ErrReadOnly
			}
		}
		if i+1 >= len(s.Tokens) || !IsWord(s.Tokens[i+1], "(") || token.Value == "" || !wordStart(token.Value[0]) && !token.Quoted {
			continue
		}
		if token.Quoted || i > 0 && IsWord(s.Tokens[i-1], ".") {
			return Statement{}, ErrReadOnly
		}

		switch strings.ToUpper(token.Value) {
		case "SELECT", "FROM", "WHERE", "AS", "IN", "NOT", "AND", "OR", "ON", "EXISTS", "OVER", "FILTER", "BY", "WHEN", "THEN", "ELSE", "DISTINCT", "ALL", "UNION", "INTERSECT", "EXCEPT", "LATERAL", "USING", "VALUES":
			continue
		}
		if !readFunctions[strings.ToUpper(token.Value)] {
			return Statement{}, ErrReadOnly
		}
	}
	return s, nil
}

var readFunctions = func() map[string]bool {
	functions := make(map[string]bool)
	for _, name := range strings.Fields(`COUNT SUM AVG MIN MAX ABS ROUND CEIL CEILING FLOOR MOD POWER SQRT
		LOWER UPPER LENGTH CHAR_LENGTH CHARACTER_LENGTH OCTET_LENGTH TRIM LTRIM RTRIM
		SUBSTRING SUBSTR REPLACE CONCAT CONCAT_WS LEFT RIGHT POSITION STRPOS
		COALESCE NULLIF GREATEST LEAST CAST CONVERT EXTRACT DATE DATE_TRUNC DATE_PART
		NOW CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_SETTING TO_CHAR TO_DATE TO_TIMESTAMP
		DATE_FORMAT YEAR MONTH DAY HOUR MINUTE SECOND DATEDIFF TIMESTAMPDIFF
		ROW_NUMBER RANK DENSE_RANK LAG LEAD FIRST_VALUE LAST_VALUE NTILE
		STRING_AGG GROUP_CONCAT ARRAY_AGG BOOL_AND BOOL_OR EVERY
		JSON_AGG JSONB_AGG JSON_BUILD_OBJECT JSONB_BUILD_OBJECT JSON_EXTRACT JSON_UNQUOTE`) {
		functions[name] = true
	}
	return functions
}()
