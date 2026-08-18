package docker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PythonString matches Python's str() for values accepted by Docker API
// option dictionaries. Top-level booleans use Docker's lowercase spelling.
func PythonString(value any) string {
	return pythonString(value, false)
}

// PythonToText matches Ansible's to_text coercion for string-typed mapping
// values. In particular, top-level booleans retain Python's True/False
// spelling, while strings remain unquoted.
func PythonToText(value any) string {
	if typed, ok := value.(bool); ok {
		if typed {
			return "True"
		}
		return "False"
	}
	return pythonString(value, false)
}

// PythonRepr matches Python's repr() for values shown in validation errors.
func PythonRepr(value any) string {
	return pythonString(value, true)
}

func pythonString(value any, nested bool) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		if nested {
			return pythonStringRepr(typed)
		}
		return typed
	case bool:
		if nested {
			if typed {
				return "True"
			}
			return "False"
		}
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case float64:
		text := strconv.FormatFloat(typed, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		return text
	case []any:
		items := make([]string, len(typed))
		for index, item := range typed {
			items[index] = pythonString(item, true)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		return pythonMapString(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func pythonMapString(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, pythonStringRepr(key)+": "+pythonString(values[key], true))
	}
	return "{" + strings.Join(items, ", ") + "}"
}

func pythonStringRepr(value string) string {
	quote := byte('\'')
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = '"'
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	escaped = strings.ReplaceAll(escaped, "\r", `\r`)
	escaped = strings.ReplaceAll(escaped, "\t", `\t`)
	escaped = strings.ReplaceAll(escaped, string(quote), `\`+string(quote))
	return string(quote) + escaped + string(quote)
}
