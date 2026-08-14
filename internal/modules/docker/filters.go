package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/moby/moby/client"
)

// FilterValues is a Docker filter term that accepts a string or a list of
// strings, matching community.docker label and until filters.
type FilterValues []string

func (values *FilterValues) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*values = nil
		return nil
	}
	if data[0] == '[' {
		var items []any
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("filter values must be a string or list: %w", err)
		}
		result := make([]string, 0, len(items))
		for _, item := range items {
			text, err := stringifyFilterValue(item)
			if err != nil {
				return err
			}
			result = append(result, text)
		}
		*values = result
		return nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	text, err := stringifyFilterValue(value)
	if err != nil {
		return err
	}
	*values = FilterValues{text}
	return nil
}

// FilterMap is a dictionary of Docker API filters. Boolean values are converted
// to "true"/"false" strings as upstream's clean_dict_booleans_for_docker_api does.
type FilterMap map[string]FilterValues

func (filters FilterMap) ToClientFilters() client.Filters {
	result := make(client.Filters)
	for key, values := range filters {
		if len(values) == 0 {
			continue
		}
		result.Add(key, values...)
	}
	return result
}

func stringifyFilterValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), nil
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case json.Number:
		return typed.String(), nil
	default:
		return "", fmt.Errorf("filter values must be strings, booleans, or numbers, got %T", value)
	}
}

// StringifyAPIMap converts a loosely typed Docker option dictionary into the
// string map Engine APIs expect. Booleans become "true"/"false".
func StringifyAPIMap(values map[string]any) (map[string]string, error) {
	if values == nil {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		text, err := stringifyFilterValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		result[key] = text
	}
	return result, nil
}
