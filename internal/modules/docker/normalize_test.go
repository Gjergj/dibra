package docker

import (
	"testing"
)

func TestNormalizeEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: nil,
		},
		{
			name:     "already sorted",
			input:    []string{"A=1", "B=2", "C=3"},
			expected: []string{"A=1", "B=2", "C=3"},
		},
		{
			name:     "needs sorting",
			input:    []string{"C=3", "A=1", "B=2"},
			expected: []string{"A=1", "B=2", "C=3"},
		},
		{
			name:     "single element",
			input:    []string{"FOO=bar"},
			expected: []string{"FOO=bar"},
		},
		{
			name:     "case sensitive",
			input:    []string{"a=1", "A=2", "B=3"},
			expected: []string{"A=2", "B=3", "a=1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeEnv(tt.input)
			if !CompareStringSlicesOrdered(result, tt.expected) {
				t.Errorf("NormalizeEnv(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCompareStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "same order",
			a:        []string{"a", "b", "c"},
			b:        []string{"a", "b", "c"},
			expected: true,
		},
		{
			name:     "different order same elements",
			a:        []string{"c", "a", "b"},
			b:        []string{"a", "b", "c"},
			expected: true,
		},
		{
			name:     "different lengths",
			a:        []string{"a", "b"},
			b:        []string{"a", "b", "c"},
			expected: false,
		},
		{
			name:     "different elements",
			a:        []string{"a", "b", "c"},
			b:        []string{"a", "b", "d"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareStringSlices(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("CompareStringSlices(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestCompareMaps(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]string
		b        map[string]string
		expected bool
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "both empty",
			a:        map[string]string{},
			b:        map[string]string{},
			expected: true,
		},
		{
			name:     "same content",
			a:        map[string]string{"foo": "bar", "baz": "qux"},
			b:        map[string]string{"baz": "qux", "foo": "bar"},
			expected: true,
		},
		{
			name:     "different values",
			a:        map[string]string{"foo": "bar"},
			b:        map[string]string{"foo": "baz"},
			expected: false,
		},
		{
			name:     "different keys",
			a:        map[string]string{"foo": "bar"},
			b:        map[string]string{"baz": "bar"},
			expected: false,
		},
		{
			name:     "different sizes",
			a:        map[string]string{"foo": "bar"},
			b:        map[string]string{"foo": "bar", "baz": "qux"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareMaps(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("CompareMaps(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestEnvSliceToMap(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]string
	}{
		{
			name:     "empty",
			input:    []string{},
			expected: map[string]string{},
		},
		{
			name:     "single var",
			input:    []string{"FOO=bar"},
			expected: map[string]string{"FOO": "bar"},
		},
		{
			name:     "multiple vars",
			input:    []string{"FOO=bar", "BAZ=qux"},
			expected: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:     "value with equals",
			input:    []string{"FOO=bar=baz"},
			expected: map[string]string{"FOO": "bar=baz"},
		},
		{
			name:     "empty value",
			input:    []string{"FOO="},
			expected: map[string]string{"FOO": ""},
		},
		{
			name:     "no equals",
			input:    []string{"FOO"},
			expected: map[string]string{"FOO": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnvSliceToMap(tt.input)
			if !CompareMaps(result, tt.expected) {
				t.Errorf("EnvSliceToMap(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEnvMapToSlice(t *testing.T) {
	input := map[string]string{"FOO": "bar", "BAZ": "qux"}
	result := EnvMapToSlice(input)

	// Should be sorted
	expected := []string{"BAZ=qux", "FOO=bar"}
	if !CompareStringSlicesOrdered(result, expected) {
		t.Errorf("EnvMapToSlice(%v) = %v, want %v", input, result, expected)
	}
}

func TestMergeMaps(t *testing.T) {
	a := map[string]string{"foo": "bar", "keep": "original"}
	b := map[string]string{"baz": "qux", "keep": "override"}

	result := MergeMaps(a, b)

	expected := map[string]string{"foo": "bar", "baz": "qux", "keep": "override"}
	if !CompareMaps(result, expected) {
		t.Errorf("MergeMaps result = %v, want %v", result, expected)
	}
}

func TestDiffBuilder(t *testing.T) {
	t.Run("empty builder", func(t *testing.T) {
		db := NewDiffBuilder()
		if db.HasDiffs() {
			t.Error("Expected no diffs on new builder")
		}
	})

	t.Run("add diffs", func(t *testing.T) {
		db := NewDiffBuilder()
		db.Add("field1", "new", "old")
		db.AddIfDifferentStr("field2", "different", "original")
		db.AddIfDifferentStr("field3", "same", "same") // Should not add

		if !db.HasDiffs() {
			t.Error("Expected diffs")
		}

		diffs := db.Diffs()
		if len(diffs) != 2 {
			t.Errorf("Expected 2 diffs, got %d", len(diffs))
		}
	})

	t.Run("diff map", func(t *testing.T) {
		db := NewDiffBuilder()
		db.Add("image", "alpine:3.18", "alpine:3.17")

		diffMap := db.DiffMap()
		if _, ok := diffMap["image"]; !ok {
			t.Error("Expected 'image' key in diff map")
		}
	})
}

func TestStringSliceContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !StringSliceContains(slice, "a") {
		t.Error("Expected slice to contain 'a'")
	}
	if StringSliceContains(slice, "d") {
		t.Error("Expected slice to not contain 'd'")
	}
}

func TestStringSliceContainsAny(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !StringSliceContainsAny(slice, "x", "y", "b") {
		t.Error("Expected slice to contain one of x, y, b")
	}
	if StringSliceContainsAny(slice, "x", "y", "z") {
		t.Error("Expected slice to not contain any of x, y, z")
	}
}
