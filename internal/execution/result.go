package execution

import (
	"encoding/json"
	"fmt"
)

// RedactedValue is the stable replacement used when a registry definition
// marks an argument or result path as sensitive.
const RedactedValue = "VALUE_SPECIFIED_IN_NO_LOG_PARAMETER"

// Result is the common portion of every module response. Module-specific
// return fields are carried alongside these fields on the JSON object.
type Result struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg"`
}

// Diff is the common structured diff contract. Before and After can be
// strings, objects, or other JSON-compatible values depending on the module.
type Diff struct {
	Before any `json:"before"`
	After  any `json:"after"`
}

// Failure returns a standard failed module result.
func Failure(msg string) Result {
	return Result{Failed: true, Msg: msg}
}

// NormalizeResult converts a typed or generic module response into the stable
// result envelope while preserving all module-specific return fields.
func NormalizeResult(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal module result: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode module result object: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("module result must be a JSON object")
	}

	for _, field := range []string{"changed", "failed"} {
		if raw, ok := result[field]; ok {
			if _, valid := raw.(bool); !valid {
				return nil, fmt.Errorf("module result field %q must be a boolean", field)
			}
		} else {
			result[field] = false
		}
	}

	if raw, ok := result["msg"]; ok {
		if raw == nil {
			result["msg"] = ""
		} else if _, valid := raw.(string); !valid {
			return nil, fmt.Errorf("module result field %q must be a string", "msg")
		}
	} else {
		result["msg"] = ""
	}

	if raw, ok := result["diff"]; ok {
		normalized, keep, err := normalizeDiff(raw)
		if err != nil {
			return nil, err
		}
		if keep {
			result["diff"] = normalized
		} else {
			delete(result, "diff")
		}
	}

	return result, nil
}

func normalizeDiff(raw any) (map[string]any, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	diff, ok := raw.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("module result field %q must be an object with before and after fields", "diff")
	}
	if len(diff) == 0 {
		return nil, false, nil
	}

	_, hasBefore := diff["before"]
	_, hasAfter := diff["after"]
	if hasBefore || hasAfter {
		if !hasBefore {
			diff["before"] = nil
		}
		if !hasAfter {
			diff["after"] = nil
		}
		return diff, true, nil
	}

	// Older docker_swarm_service agents returned only a summary list. The
	// original values cannot be recovered, but preserving the summary under
	// before/after keeps mixed controller/agent versions interoperable.
	if len(diff) == 1 {
		if rawFields, ok := diff["changed_fields"]; ok {
			fields, valid := stringSlice(rawFields)
			if !valid {
				return nil, false, fmt.Errorf("module result diff field %q must be a list of strings", "changed_fields")
			}
			return map[string]any{
				"before": map[string]any{"changed_fields": []string{}},
				"after":  map[string]any{"changed_fields": fields},
			}, true, nil
		}
	}

	// Accept the original Dibra field-centric shape at the boundary so older
	// agents remain compatible, but always expose the canonical shape.
	before := make(map[string]any, len(diff))
	after := make(map[string]any, len(diff))
	for field, rawChange := range diff {
		change, ok := rawChange.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("module result diff field %q must contain before and after values", field)
		}
		before[field] = change["before"]
		after[field] = change["after"]
	}
	return map[string]any{"before": before, "after": after}, true, nil
}

func stringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		result[index] = text
	}
	return result, true
}
