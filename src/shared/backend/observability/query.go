package observability

import (
	"context"
	"strings"
)

// MigrationContext follows an operation from enqueue through worker and batches.
// Empty fields inherit the parent context; SQL and parameter values never enter it.
type MigrationContext struct {
	DefinitionID, RevisionID      int64
	JobID, ExecutionID, Operation string
	Attempt                       int
	Batch                         int64
}
type migrationKey struct{}
type dialectKey struct{}

// Only the selected server adapter supplies the dialect, never an HTTP header.
func WithSQLDialect(ctx context.Context, dialect string) context.Context {
	return context.WithValue(ctx, dialectKey{}, dialect)
}
func SQLText(ctx context.Context, query string) string {
	dialect, _ := ctx.Value(dialectKey{}).(string)
	return safeSQL(query, dialect == "postgresql")
}

func WithMigration(ctx context.Context, next MigrationContext) context.Context {
	value, _ := ctx.Value(migrationKey{}).(MigrationContext)
	if next.DefinitionID > 0 {
		value.DefinitionID = next.DefinitionID
	}
	if next.RevisionID > 0 {
		value.RevisionID = next.RevisionID
	}
	if next.JobID != "" {
		value.JobID = next.JobID
	}
	if next.ExecutionID != "" {
		value.ExecutionID = next.ExecutionID
	}
	if next.Operation != "" {
		value.Operation = next.Operation
	}
	if next.Attempt > 0 {
		value.Attempt = next.Attempt
	}
	if next.Batch > 0 {
		value.Batch = next.Batch
	}
	return context.WithValue(ctx, migrationKey{}, value)
}

// SafeSQL preserves query structure, unquoted identifiers and placeholders while masking
// literals and comments. It handles nested comments, escaped/doubled quotes and
// PostgreSQL dollar quoting. Unterminated tokens consume the rest, failing closed.
func SafeSQL(query string) string {
	return safeSQL(query, false)
}
func safeSQL(query string, quotedIdentifiers bool) string {
	var out strings.Builder
	for i := 0; i < len(query); {
		switch {
		case strings.HasPrefix(query[i:], "--") || query[i] == '#':
			end := strings.IndexByte(query[i:], '\n')
			out.WriteString("/* redacted */")
			if end < 0 {
				return out.String()
			}
			i += end
		case strings.HasPrefix(query[i:], "/*"):
			i += 2
			depth := 1
			for i < len(query) && depth > 0 {
				if strings.HasPrefix(query[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(query[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			out.WriteString("/* redacted */")
		case query[i] == '\'':
			i = quotedEnd(query, i, '\'')
			out.WriteString("'[redacted]'")
		case query[i] == '"':
			// Double quotes can enclose values in MySQL's default SQL mode.
			end := quotedEnd(query, i, '"')
			if quotedIdentifiers {
				var closed bool
				end, closed = nameEnd(query, i, '"')
				if closed {
					out.WriteString(query[i:end])
				} else {
					out.WriteString(`"[redacted]"`)
				}
			} else {
				out.WriteString(`"[redacted]"`)
			}
			i = end
		case query[i] == '`':
			end, closed := nameEnd(query, i, '`')
			if closed {
				out.WriteString(query[i:end])
			} else {
				out.WriteString("`[redacted]`")
			}
			i = end
		case query[i] == '$':
			end := i + 1
			for end < len(query) && (query[end] == '_' || query[end] >= 'a' && query[end] <= 'z' || query[end] >= 'A' && query[end] <= 'Z' || end > i+1 && query[end] >= '0' && query[end] <= '9') {
				end++
			}
			if end < len(query) && query[end] == '$' {
				tag := query[i : end+1]
				close := strings.Index(query[end+1:], tag)
				out.WriteString("'[redacted]'")
				if close < 0 {
					return out.String()
				}
				i = end + 1 + close + len(tag)
			} else {
				out.WriteByte('$')
				i++
				for i < len(query) && query[i] >= '0' && query[i] <= '9' {
					out.WriteByte(query[i])
					i++
				}
			}
		case query[i] >= '0' && query[i] <= '9' && (i == 0 || !identifierByte(query[i-1])):
			out.WriteByte('?')
			i++
			for i < len(query) && (identifierByte(query[i]) || query[i] == '.' || (query[i] == '+' || query[i] == '-') && (query[i-1] == 'e' || query[i-1] == 'E')) {
				i++
			}
		default:
			out.WriteByte(query[i])
			i++
		}
	}
	return out.String()
}
func identifierByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c >= 128
}
func quotedEnd(query string, start int, quote byte) int {
	for i := start + 1; i < len(query); i++ {
		if query[i] == '\\' {
			i++
			continue
		}
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(query)
}

// PostgreSQL quoted identifiers do not use backslash escapes. MySQL backtick
// identifiers do; neither path emits the tail of an unterminated identifier.
func nameEnd(query string, start int, quote byte) (int, bool) {
	for i := start + 1; i < len(query); i++ {
		if quote == '`' && query[i] == '\\' {
			i++
			continue
		}
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			return i + 1, true
		}
	}
	return len(query), false
}

func SQLStart(ctx context.Context, query string, parameters int) {
	Event(ctx, "sql.start", nil, "query", SQLText(ctx, query), "parameter_count", parameters)
}
