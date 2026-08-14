package docker

import "encoding/json"

// InspectionMap encodes a Docker Engine object into a generic map using the
// daemon's original JSON field names. Prefer Raw inspection bytes when the
// client already captured them.
func InspectionMap(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return DecodeInspection(encoded)
}

// DecodeInspection turns raw Engine JSON into a map. An empty or null payload
// becomes an empty map rather than a nil result.
func DecodeInspection(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

// InspectionSlice encodes a slice of Engine objects into generic maps.
func InspectionSlice(value any) ([]map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) == 0 || string(encoded) == "null" {
		return []map[string]any{}, nil
	}
	var result []map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return []map[string]any{}, nil
	}
	return result, nil
}
