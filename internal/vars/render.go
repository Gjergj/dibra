package vars

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var templatePattern = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func RenderValue(value interface{}, context map[string]interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return RenderString(v, context)
	case []interface{}:
		rendered := make([]interface{}, len(v))
		for i, item := range v {
			next, err := RenderValue(item, context)
			if err != nil {
				return nil, err
			}
			rendered[i] = next
		}
		return rendered, nil
	case map[string]interface{}:
		rendered := map[string]interface{}{}
		for key, item := range v {
			next, err := RenderValue(item, context)
			if err != nil {
				return nil, err
			}
			rendered[key] = next
		}
		return rendered, nil
	default:
		return value, nil
	}
}

func RenderString(input string, context map[string]interface{}) (string, error) {
	const maxIterations = 10
	result := input
	for i := 0; i < maxIterations; i++ {
		matches := templatePattern.FindAllStringSubmatchIndex(result, -1)
		if len(matches) == 0 {
			return result, nil
		}

		var builder strings.Builder
		last := 0
		for _, match := range matches {
			builder.WriteString(result[last:match[0]])
			expr := strings.TrimSpace(result[match[2]:match[3]])
			resolved, ok := resolveExpr(expr, context)
			if !ok {
				return "", fmt.Errorf("unknown variable %q", expr)
			}
			builder.WriteString(fmt.Sprintf("%v", resolved))
			last = match[1]
		}
		builder.WriteString(result[last:])
		next := builder.String()
		if next == result {
			return result, nil
		}
		result = next
	}
	return result, nil
}

// RenderTemplateValue renders a templated string and returns the resolved value
// when the input is a single template expression ("{{ var }}").
// If the input contains additional text, it falls back to RenderString.
func RenderTemplateValue(input string, context map[string]interface{}) (interface{}, error) {
	trimmed := strings.TrimSpace(input)
	matches := templatePattern.FindAllStringSubmatch(trimmed, -1)
	if len(matches) == 1 && strings.TrimSpace(matches[0][0]) == trimmed {
		expr := strings.TrimSpace(matches[0][1])
		resolved, ok := resolveExpr(expr, context)
		if !ok {
			return nil, fmt.Errorf("unknown variable %q", expr)
		}
		return resolved, nil
	}
	return RenderString(input, context)
}

func resolveExpr(expr string, context map[string]interface{}) (interface{}, bool) {
	tokens, err := tokenize(expr)
	if err != nil {
		return nil, false
	}
	if len(tokens) == 0 {
		return nil, false
	}
	current := interface{}(context)
	for _, token := range tokens {
		key := token.value
		if token.dynamic {
			resolved, ok := resolveExpr(token.value, context)
			if !ok {
				return nil, false
			}
			key = fmt.Sprint(resolved)
		}

		switch typed := current.(type) {
		case map[string]interface{}:
			next, ok := typed[key]
			if !ok {
				return nil, false
			}
			current = next
		case []interface{}:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		case []string:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

type expressionToken struct {
	value   string
	dynamic bool
}

func tokenize(expr string) ([]expressionToken, error) {
	var tokens []expressionToken
	var current strings.Builder
	i := 0
	for i < len(expr) {
		ch := expr[i]
		switch ch {
		case '.':
			if current.Len() == 0 {
				if len(tokens) == 0 {
					return nil, fmt.Errorf("invalid expression %q", expr)
				}
				i++
				continue
			}
			tokens = append(tokens, expressionToken{value: current.String()})
			current.Reset()
			i++
		case '[':
			if current.Len() > 0 {
				tokens = append(tokens, expressionToken{value: current.String()})
				current.Reset()
			}
			end := strings.IndexByte(expr[i:], ']')
			if end == -1 {
				return nil, fmt.Errorf("invalid expression %q", expr)
			}
			content := strings.TrimSpace(expr[i+1 : i+end])
			if content == "" {
				return nil, fmt.Errorf("invalid expression %q", expr)
			}

			dynamic := true
			if len(content) >= 2 {
				first, last := content[0], content[len(content)-1]
				if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
					content = content[1 : len(content)-1]
					dynamic = false
				}
			}
			if _, err := strconv.Atoi(content); err == nil {
				dynamic = false
			}
			tokens = append(tokens, expressionToken{value: content, dynamic: dynamic})
			i += end + 1
		case ' ', '\t', '\n', '\r':
			i++
		default:
			current.WriteByte(ch)
			i++
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, expressionToken{value: current.String()})
	}
	return tokens, nil
}
