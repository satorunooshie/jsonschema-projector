package pointer

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Pointer struct {
	Raw    string
	Tokens []string
}

func Parse(raw string) (Pointer, error) {
	p := Pointer{Raw: raw}

	switch {
	case raw == "":
		return p, nil
	case raw == "#":
		return p, nil
	case strings.HasPrefix(raw, "#"):
		raw = strings.TrimPrefix(raw, "#")
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			return Pointer{}, fmt.Errorf("invalid URI fragment %q: %w", p.Raw, err)
		}
		raw = decoded
	case strings.HasPrefix(raw, "/"):
	default:
		return Pointer{}, fmt.Errorf("pointer must be an empty string, a JSON pointer, or a local fragment: %q", p.Raw)
	}

	if raw == "" {
		return p, nil
	}
	if !strings.HasPrefix(raw, "/") {
		return Pointer{}, fmt.Errorf("invalid pointer %q", p.Raw)
	}

	parts := strings.Split(raw[1:], "/")
	tokens := make([]string, len(parts))
	for i, part := range parts {
		token, err := unescape(part)
		if err != nil {
			return Pointer{}, fmt.Errorf("invalid pointer %q: %w", p.Raw, err)
		}
		tokens[i] = token
	}

	p.Tokens = tokens
	return p, nil
}

func Eval(doc any, raw string) (any, bool, error) {
	p, err := Parse(raw)
	if err != nil {
		return nil, false, err
	}
	return p.Eval(doc)
}

func (p Pointer) Eval(doc any) (any, bool, error) {
	current := doc
	for _, token := range p.Tokens {
		switch value := current.(type) {
		case map[string]any:
			next, ok := value[token]
			if !ok {
				return nil, false, nil
			}
			current = next
		case []any:
			if token == "-" {
				return nil, false, nil
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false, nil
			}
			current = value[index]
		default:
			return nil, false, nil
		}
	}

	return current, true, nil
}

func EscapeToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	token = strings.ReplaceAll(token, "/", "~1")
	return token
}

func Join(tokens ...string) string {
	if len(tokens) == 0 {
		return "#"
	}

	var b strings.Builder
	b.WriteByte('#')
	for _, token := range tokens {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(EscapeToken(token)))
	}
	return b.String()
}

func IsLocalFragment(ref string) bool {
	if ref == "#" || strings.HasPrefix(ref, "#/") {
		return true
	}
	if !strings.HasPrefix(ref, "#") {
		return false
	}

	fragment := strings.TrimPrefix(ref, "#")
	decoded, err := url.PathUnescape(fragment)
	return err == nil && strings.HasPrefix(decoded, "/")
}

func unescape(token string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			b.WriteByte(token[i])
			continue
		}

		if i+1 >= len(token) {
			return "", fmt.Errorf("dangling escape in token %q", token)
		}
		switch token[i+1] {
		case '0':
			b.WriteByte('~')
		case '1':
			b.WriteByte('/')
		default:
			return "", fmt.Errorf("invalid escape ~%c in token %q", token[i+1], token)
		}
		i++
	}

	return b.String(), nil
}
