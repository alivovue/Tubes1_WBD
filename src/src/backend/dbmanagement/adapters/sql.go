package adapters

import (
	"strings"
	"unicode/utf8"
)

func wordStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128
}
func wordPart(c byte) bool { return wordStart(c) || c >= '0' && c <= '9' || c == '$' }
func space(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func ParseSQL(query, dbms string) ([]Statement, error) {
	if len(query) == 0 || len(query) > 1<<20 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return nil, ErrInvalid
	}
	maria := Normalize(dbms) == "mariadb"
	result := make([]Statement, 0)
	tokens := make([]Token, 0)
	start := 0
	hasComments := false
	emit := func(end int) error {
		if len(tokens) == 0 {
			return nil
		}
		if tokens[0].Quoted || tokens[0].Literal {
			return ErrInvalid
		}
		command := strings.ToUpper(tokens[0].Value)
		explainable := command == "SELECT" || command == "INSERT" || command == "UPDATE" || command == "DELETE" || command == "WITH" || command == "MERGE" || command == "REPLACE"
		result = append(result, Statement{HasComments: hasComments, SQL: strings.TrimSpace(query[start:end]), Command: command, Explainable: explainable, Tokens: append([]Token(nil), tokens...)})
		tokens = tokens[:0]
		hasComments = false
		return nil
	}
	for i := 0; i < len(query); {
		c := query[i]
		if space(c) {
			i++
			continue
		}
		if maria && c == '#' || c == '-' && i+1 < len(query) && query[i+1] == '-' && (!maria || i+2 == len(query) || space(query[i+2])) {
			hasComments = true
			i++
			for i < len(query) && query[i] != '\n' && query[i] != '\r' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(query) && query[i+1] == '*' {
			hasComments = true
			if maria && i+2 < len(query) && (query[i+2] == '!' || i+3 < len(query) && (query[i+2] == 'M' || query[i+2] == 'm') && query[i+3] == '!') {
				return nil, ErrInvalid
			}
			i += 2
			depth := 1
			for i < len(query) && depth > 0 {
				if i+1 < len(query) && query[i:i+2] == "/*" {
					if maria {
						return nil, ErrInvalid
					}
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
		if c == '\'' || c == '"' || maria && c == '`' {
			quote := c
			literal := quote == '\'' || maria && quote == '"'
			begin := i
			escaped := !maria && quote == '\'' && i > 0 && (query[i-1] == 'E' || query[i-1] == 'e') && (i == 1 || !wordPart(query[i-2]))
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
			if literal {
				tokens = append(tokens, Token{Value: query[begin:i], Literal: true})
			} else {
				tokens = append(tokens, Token{Value: value.String(), Quoted: true})
			}
			continue
		}
		if !maria && c == '$' {
			end := i + 1
			if end < len(query) && wordStart(query[end]) {
				end++
				for end < len(query) && (wordStart(query[end]) || query[end] >= '0' && query[end] <= '9') {
					end++
				}
			}
			if end < len(query) && query[end] == '$' {
				tag := query[i : end+1]
				closing := strings.Index(query[end+1:], tag)
				if closing < 0 {
					return nil, ErrInvalid
				}
				end = end + 1 + closing + len(tag)
				tokens = append(tokens, Token{Value: query[i:end], Literal: true})
				i = end
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
		if wordStart(c) {
			end := i + 1
			for end < len(query) && wordPart(query[end]) {
				end++
			}
			tokens = append(tokens, Token{Value: query[i:end]})
			i = end
			continue
		}
		tokens = append(tokens, Token{Value: string(c)})
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

func ValidIdentifier(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
func IsWord(t Token, value string) bool {
	return !t.Quoted && !t.Literal && strings.EqualFold(t.Value, value)
}

func ValidateMariaReferences(database string, s Statement) error {
	tokens := s.Tokens
	depth := 0
	from := map[int]bool{}
	for position, token := range tokens {
		if token.Literal || token.Quoted {
			continue
		}
		word := strings.ToUpper(token.Value)
		switch word {
		case "(":
			depth++
		case ")":
			delete(from, depth)
			depth--
		case "FROM":
			from[depth] = true
		case "UPDATE":
			from[depth] = !(position > 0 && IsWord(tokens[position-1], "KEY"))
		case "WHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "SET", "VALUES", "RETURNING", "UNION":
			from[depth] = false
		case ",":
			if from[depth] {
				return ErrUnsupported
			}
		}
	}

	aliases := map[string]bool{}
	for i, t := range tokens {
		if t.Literal || t.Quoted {
			continue
		}
		word := strings.ToUpper(t.Value)
		if word != "FROM" && word != "JOIN" && word != "UPDATE" && word != "INTO" && word != "TABLE" && word != "REFERENCES" && word != "USING" {
			continue
		}
		j := i + 1
		for j < len(tokens) && (IsWord(tokens[j], "IF") || IsWord(tokens[j], "NOT") || IsWord(tokens[j], "EXISTS")) {
			j++
		}
		if j >= len(tokens) {
			continue
		}
		if tokens[j].Value == "(" {
			depth := 1
			j++
			for j < len(tokens) && depth > 0 {
				if !tokens[j].Literal && !tokens[j].Quoted {
					if tokens[j].Value == "(" {
						depth++
					}
					if tokens[j].Value == ")" {
						depth--
					}
				}
				j++
			}
			if depth != 0 {
				return ErrInvalid
			}
			if j < len(tokens) && IsWord(tokens[j], "AS") {
				j++
			}
			if j < len(tokens) && !tokens[j].Literal {
				aliases[tokens[j].Value] = true
			}
			continue
		}
		if j+2 < len(tokens) && tokens[j+1].Value == "." {
			if tokens[j].Value != database {
				return ErrInvalid
			}
			j += 2
		}
		if !tokens[j].Literal && tokens[j].Value != "(" {
			aliases[tokens[j].Value] = true
		}
		j++
		if j < len(tokens) && IsWord(tokens[j], "AS") {
			j++
		}
		if j < len(tokens) && !tokens[j].Literal && tokens[j].Value != "(" && tokens[j].Value != "," {
			aliases[tokens[j].Value] = true
		}
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Literal || tokens[i+1].Value != "." {
			continue
		}
		if tokens[i].Value != database && !aliases[tokens[i].Value] {
			return ErrInvalid
		}
		if i+4 < len(tokens) && tokens[i+3].Value == "." && tokens[i].Value != database {
			return ErrInvalid
		}
	}
	return nil
}
