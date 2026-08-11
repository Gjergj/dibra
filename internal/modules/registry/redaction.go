package registry

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
)

// RedactArguments returns a JSON-compatible copy of arguments with every
// sensitive registry path replaced. The original arguments are not modified.
func RedactArguments(name string, arguments any) (any, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown registered module %q", name)
	}
	clone, err := cloneJSONValue(arguments)
	if err != nil {
		return nil, err
	}
	secrets := collectPathValues(clone, definition.Sensitivity.Arguments)
	scrubStrings(clone, secrets)
	redactPaths(clone, definition.Sensitivity.Arguments)
	return clone, nil
}

// RedactResult returns a display-safe copy of a normalized module result.
// Sensitive values are removed at their declared paths and scrubbed anywhere
// else a module may have echoed them, such as msg, stdout, or stderr.
func RedactResult(name string, arguments any, result map[string]any) (map[string]any, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown registered module %q", name)
	}
	argumentClone, err := cloneJSONValue(arguments)
	if err != nil {
		return nil, err
	}
	resultClone, err := cloneJSONValue(result)
	if err != nil {
		return nil, err
	}
	redacted, ok := resultClone.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("module result must be a JSON object")
	}

	secrets := collectPathValues(argumentClone, definition.Sensitivity.Arguments)
	secrets = append(secrets, collectPathValues(redacted, definition.Sensitivity.Results)...)
	scrubStrings(redacted, secrets)
	redactPaths(redacted, definition.Sensitivity.Results)
	return redacted, nil
}

// RedactText scrubs sensitive argument values from an error or malformed
// response before it is written to controller output.
func RedactText(name string, arguments any, value string) string {
	definition, ok := Lookup(name)
	if !ok {
		return value
	}
	clone, err := cloneJSONValue(arguments)
	if err != nil {
		return execution.RedactedValue
	}
	return scrubText(value, collectPathValues(clone, definition.Sensitivity.Arguments))
}

func cloneJSONValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal value for redaction: %w", err)
	}
	var clone any
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("decode value for redaction: %w", err)
	}
	return clone, nil
}

func collectPathValues(root any, paths []string) []string {
	var values []string
	for _, path := range paths {
		collectAtPath(root, strings.Split(path, "."), &values)
	}
	unique := make(map[string]bool, len(values))
	rawValues := values
	values = make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		if value != "" && value != execution.RedactedValue && !unique[value] {
			unique[value] = true
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

func collectAtPath(value any, path []string, values *[]string) {
	if len(path) == 0 {
		collectStrings(value, values)
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if next, ok := typed[strings.Join(path, ".")]; ok {
			collectStrings(next, values)
			return
		}
		if next, ok := typed[path[0]]; ok {
			collectAtPath(next, path[1:], values)
		}
	case []any:
		for _, item := range typed {
			collectAtPath(item, path, values)
		}
	}
}

func collectStrings(value any, values *[]string) {
	switch typed := value.(type) {
	case string:
		*values = append(*values, typed)
	case map[string]any:
		for _, item := range typed {
			collectStrings(item, values)
		}
	case []any:
		for _, item := range typed {
			collectStrings(item, values)
		}
	}
}

func redactPaths(root any, paths []string) {
	for _, path := range paths {
		redactAtPath(root, strings.Split(path, "."))
	}
}

func redactAtPath(value any, path []string) {
	if len(path) == 0 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		joined := strings.Join(path, ".")
		if _, ok := typed[joined]; ok {
			typed[joined] = execution.RedactedValue
			return
		}
		if len(path) == 1 {
			if _, ok := typed[path[0]]; ok {
				typed[path[0]] = execution.RedactedValue
			}
			return
		}
		if next, ok := typed[path[0]]; ok {
			redactAtPath(next, path[1:])
		}
	case []any:
		for _, item := range typed {
			redactAtPath(item, path)
		}
	}
}

func scrubStrings(value any, secrets []string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok {
				typed[key] = scrubText(text, secrets)
				continue
			}
			scrubStrings(item, secrets)
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok {
				typed[index] = scrubText(text, secrets)
				continue
			}
			scrubStrings(item, secrets)
		}
	}
}

func scrubText(value string, secrets []string) string {
	if value == "" || len(secrets) == 0 {
		return value
	}
	quoted := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			quoted = append(quoted, regexp.QuoteMeta(secret))
		}
	}
	if len(quoted) == 0 {
		return value
	}
	return regexp.MustCompile(strings.Join(quoted, "|")).ReplaceAllString(value, execution.RedactedValue)
}
