package docker

import (
	"sort"
	"strings"
)

// NormalizeEnv sorts environment variables by key for consistent comparison.
// Input format: ["KEY=value", "KEY2=value2"]
func NormalizeEnv(env []string) []string {
	if len(env) == 0 {
		return env
	}
	result := make([]string, len(env))
	copy(result, env)
	sort.Strings(result)
	return result
}

// NormalizeLabels returns a copy of the labels map.
// Maps are already order-independent, but this ensures we're comparing copies.
func NormalizeLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	result := make(map[string]string, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}

// NormalizeMounts sorts mount specifications for consistent comparison.
// Input format: ["source:target:options", ...]
func NormalizeMounts(mounts []string) []string {
	if len(mounts) == 0 {
		return mounts
	}
	result := make([]string, len(mounts))
	copy(result, mounts)
	sort.Strings(result)
	return result
}

// CompareStringSlices compares two string slices for equality,
// ignoring order (set comparison).
func CompareStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}

	// Sort copies and compare
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)
	sort.Strings(aCopy)
	sort.Strings(bCopy)

	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}

// CompareStringSlicesOrdered compares two string slices for equality,
// respecting order.
func CompareStringSlicesOrdered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CompareMaps compares two string maps for equality.
func CompareMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// StringSliceContains checks if a slice contains a specific string.
func StringSliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// StringSliceContainsAny checks if a slice contains any of the given items.
func StringSliceContainsAny(slice []string, items ...string) bool {
	for _, item := range items {
		if StringSliceContains(slice, item) {
			return true
		}
	}
	return false
}

// MergeMaps merges two maps, with values from b overriding values from a.
func MergeMaps(a, b map[string]string) map[string]string {
	result := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
	}
	return result
}

// NormalizeNetworkName normalizes network names for comparison.
// Docker sometimes returns full IDs, sometimes short names.
func NormalizeNetworkName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// ExtractEnvKey extracts the key from an environment variable string "KEY=value".
func ExtractEnvKey(env string) string {
	idx := strings.Index(env, "=")
	if idx < 0 {
		return env
	}
	return env[:idx]
}

// ExtractEnvValue extracts the value from an environment variable string "KEY=value".
func ExtractEnvValue(env string) string {
	idx := strings.Index(env, "=")
	if idx < 0 {
		return ""
	}
	return env[idx+1:]
}

// EnvSliceToMap converts ["KEY=value", ...] to map[string]string.
func EnvSliceToMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, e := range env {
		key := ExtractEnvKey(e)
		value := ExtractEnvValue(e)
		result[key] = value
	}
	return result
}

// EnvMapToSlice converts map[string]string to ["KEY=value", ...].
func EnvMapToSlice(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	sort.Strings(result)
	return result
}

// Diff represents a difference between desired and current state.
type Diff struct {
	Field   string      `json:"field"`
	Desired interface{} `json:"desired,omitempty"`
	Current interface{} `json:"current,omitempty"`
}

// DiffBuilder helps build a list of differences.
type DiffBuilder struct {
	diffs []Diff
}

// NewDiffBuilder creates a new DiffBuilder.
func NewDiffBuilder() *DiffBuilder {
	return &DiffBuilder{diffs: []Diff{}}
}

// Add adds a difference to the builder.
func (b *DiffBuilder) Add(field string, desired, current interface{}) {
	b.diffs = append(b.diffs, Diff{Field: field, Desired: desired, Current: current})
}

// AddIfDifferent adds a difference only if desired != current.
func (b *DiffBuilder) AddIfDifferentStr(field string, desired, current string) bool {
	if desired != current {
		b.Add(field, desired, current)
		return true
	}
	return false
}

// AddIfDifferentInt adds a difference only if desired != current.
func (b *DiffBuilder) AddIfDifferentInt(field string, desired, current int) bool {
	if desired != current {
		b.Add(field, desired, current)
		return true
	}
	return false
}

// AddIfDifferentBool adds a difference only if desired != current.
func (b *DiffBuilder) AddIfDifferentBool(field string, desired, current bool) bool {
	if desired != current {
		b.Add(field, desired, current)
		return true
	}
	return false
}

// HasDiffs returns true if there are any differences.
func (b *DiffBuilder) HasDiffs() bool {
	return len(b.diffs) > 0
}

// Diffs returns the list of differences.
func (b *DiffBuilder) Diffs() []Diff {
	return b.diffs
}

// DiffMap returns the stable Ansible-shaped diff.before/diff.after object.
func (b *DiffBuilder) DiffMap() map[string]interface{} {
	if len(b.diffs) == 0 {
		return nil
	}
	before := make(map[string]interface{}, len(b.diffs))
	after := make(map[string]interface{}, len(b.diffs))
	for _, d := range b.diffs {
		before[d.Field] = d.Current
		after[d.Field] = d.Desired
	}
	return map[string]interface{}{"before": before, "after": after}
}
