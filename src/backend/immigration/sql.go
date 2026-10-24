package immigration

import (
	"strings"
	"unicode/utf8"
)

type Statement struct {
	SQL         string
	Command     string
	Explainable bool
}

type token struct {
	value  string
	quoted bool
}

func identifierStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128
}
func identifierPart(c byte) bool { return identifierStart(c) || c >= '0' && c <= '9' || c == '$' }

func ValidateSQL(query string) ([]Statement, error) {
	if len(query) == 0 || len(query) > 1<<20 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return nil, ErrInvalid
	}
	result := make([]Statement, 0)
	tokens := make([]token, 0)
	start := 0
	emit := func(end int) error {
		if len(tokens) == 0 {
			return nil
		}
		if tokens[0].quoted {
			return ErrInvalid
		}
		command := tokens[0].value
		switch command {
		case "CREATE", "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "SELECT", "WITH", "MERGE", "TRUNCATE", "COMMENT", "GRANT", "REVOKE", "DO", "CALL", "REFRESH":
		default:
			return ErrInvalid
		}
		for _, t := range tokens {
			if strings.EqualFold(t.value, "_schakker") {
				return ErrInvalid
			}
			if !t.quoted && (t.value == "CONCURRENTLY" || t.value == "SET_CONFIG") {
				return ErrInvalid
			}
		}
		if (command == "CREATE" || command == "ALTER" || command == "DROP") && len(tokens) > 1 {
			switch tokens[1].value {
			case "DATABASE", "TABLESPACE", "SYSTEM", "SUBSCRIPTION":
				return ErrInvalid
			}
		}
		explainable := command == "SELECT" || command == "INSERT" || command == "UPDATE" || command == "DELETE" || command == "WITH" || command == "MERGE"
		result = append(result, Statement{SQL: strings.TrimSpace(query[start:end]), Command: command, Explainable: explainable})
		tokens = tokens[:0]
		return nil
	}
	for i := 0; i < len(query); {
		c := query[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
			i++
			continue
		}
		if c == '-' && i+1 < len(query) && query[i+1] == '-' {
			i += 2
			for i < len(query) && query[i] != '\n' && query[i] != '\r' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(query) && query[i+1] == '*' {
			i += 2
			depth := 1
			for i < len(query) && depth > 0 {
				if i+1 < len(query) && query[i:i+2] == "/*" {
					depth++
					i += 2
				} else if i+1 < len(query) && query[i:i+2] == "*/" {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, ErrInvalid
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			escaped := quote == '\'' && i > 0 && (query[i-1] == 'E' || query[i-1] == 'e') && (i == 1 || !identifierPart(query[i-2]))
			i++
			var value strings.Builder
			closed := false
			for i < len(query) {
				if escaped && query[i] == '\\' {
					if i+1 >= len(query) {
						return nil, ErrInvalid
					}
					i += 2
					continue
				}
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						value.WriteByte(quote)
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				value.WriteByte(query[i])
				i++
			}
			if !closed {
				return nil, ErrInvalid
			}
			if quote == '"' {
				tokens = append(tokens, token{value: value.String(), quoted: true})
			} else {
				tokens = append(tokens, token{value: "literal", quoted: true})
			}
			continue
		}
		if c == '$' {
			end := i + 1
			if end < len(query) && identifierStart(query[end]) {
				end++
				for end < len(query) && (identifierStart(query[end]) || query[end] >= '0' && query[end] <= '9') {
					end++
				}
			}
			if end < len(query) && query[end] == '$' {
				tag := query[i : end+1]
				closing := strings.Index(query[end+1:], tag)
				if closing < 0 {
					return nil, ErrInvalid
				}
				i = end + 1 + closing + len(tag)
				tokens = append(tokens, token{value: "literal", quoted: true})
				continue
			}
		}
		if c == ';' {
			if err := emit(i); err != nil {
				return nil, err
			}
			i++
			start = i
			continue
		}
		if identifierStart(c) {
			end := i + 1
			for end < len(query) && identifierPart(query[end]) {
				end++
			}
			tokens = append(tokens, token{value: strings.ToUpper(query[i:end])})
			i = end
			continue
		}
		tokens = append(tokens, token{value: string(c)})
		i++
	}
	if err := emit(len(query)); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrInvalid
	}
	return result, nil
}
