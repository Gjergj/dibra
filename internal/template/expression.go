package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aisbergg/gonja/pkg/gonja"
	"github.com/aisbergg/gonja/pkg/gonja/exec"
)

// EvaluateExpression evaluates a Jinja-style expression and preserves its
// native JSON-compatible value. It is used by controller-side actions such as
// debug, assert, and set_fact that must not stringify whole expressions.
func EvaluateExpression(expression string, context map[string]interface{}) (interface{}, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("expression is empty")
	}

	env := gonja.NewEnvironment(gonja.OptUndefined(exec.NewChainedStrictUndefinedValue))
	registerAnsibleFilters(env)
	tpl, err := env.FromString("{{ (" + expression + ") | tojson }}")
	if err != nil {
		return nil, err
	}
	output, err := tpl.Execute(context)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(output)))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode expression result: %w", err)
	}
	return normalizeExpressionNumbers(value), nil
}

// RenderString renders a controller-side Jinja string using the same custom
// filters as when conditions and templates.
func RenderString(input string, context map[string]interface{}) (string, error) {
	const maxPasses = 10
	rendered := input
	for pass := 0; pass < maxPasses; pass++ {
		if !strings.Contains(rendered, "{{") && !strings.Contains(rendered, "{%") {
			return rendered, nil
		}
		env := gonja.NewEnvironment(gonja.OptUndefined(exec.NewChainedStrictUndefinedValue))
		registerAnsibleFilters(env)
		tpl, err := env.FromString(rendered)
		if err != nil {
			return "", err
		}
		next, err := tpl.Execute(context)
		if err != nil {
			return "", err
		}
		if next == rendered {
			return next, nil
		}
		rendered = next
	}
	return rendered, nil
}

// RenderValue recursively renders values, preserving the native value of a
// string that consists only of one template expression.
func RenderValue(value interface{}, context map[string]interface{}) (interface{}, error) {
	switch typed := value.(type) {
	case string:
		if expression, ok := wholeTemplateExpression(typed); ok {
			return EvaluateExpression(expression, context)
		}
		return RenderString(typed, context)
	case []interface{}:
		rendered := make([]interface{}, len(typed))
		for index, item := range typed {
			value, err := RenderValue(item, context)
			if err != nil {
				return nil, err
			}
			rendered[index] = value
		}
		return rendered, nil
	case []string:
		rendered := make([]interface{}, len(typed))
		for index, item := range typed {
			value, err := RenderValue(item, context)
			if err != nil {
				return nil, err
			}
			rendered[index] = value
		}
		return rendered, nil
	case map[string]interface{}:
		rendered := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			value, err := RenderValue(item, context)
			if err != nil {
				return nil, err
			}
			rendered[key] = value
		}
		return rendered, nil
	default:
		return value, nil
	}
}

func wholeTemplateExpression(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return "", false
	}
	if strings.Count(trimmed, "{{") != 1 || strings.Count(trimmed, "}}") != 1 {
		return "", false
	}
	return strings.TrimSpace(trimmed[2 : len(trimmed)-2]), true
}

func normalizeExpressionNumbers(value interface{}) interface{} {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := strconv.ParseInt(string(typed), 10, 64); err == nil {
			return int(integer)
		}
		if number, err := strconv.ParseFloat(string(typed), 64); err == nil {
			return number
		}
		return string(typed)
	case []interface{}:
		for index, item := range typed {
			typed[index] = normalizeExpressionNumbers(item)
		}
		return typed
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = normalizeExpressionNumbers(item)
		}
		return typed
	default:
		return value
	}
}
