package secrets

import (
	"fmt"
	"testing"
)

// mockProvider always returns a fixed value or error.
type mockProvider struct {
	values map[string]string
	err    error
}

func (m *mockProvider) Lookup(ref string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.values[ref]
	if !ok {
		return "", fmt.Errorf("mock: not found: %s", ref)
	}
	return v, nil
}

func TestResolveValue_PlainPassthrough(t *testing.T) {
	r := NewResolver()
	tests := []string{
		"hello",
		"",
		"some password with special chars !@#$%",
		"op-something-else",
		"bw-something",
		"no prefix here",
	}
	for _, input := range tests {
		got, err := r.ResolveValue(input)
		if err != nil {
			t.Errorf("ResolveValue(%q) error: %v", input, err)
		}
		if got != input {
			t.Errorf("ResolveValue(%q) = %q, want %q", input, got, input)
		}
	}
}

func TestResolveValue_BitwardenPrefix(t *testing.T) {
	r := NewResolver()
	r.Register("bw", &mockProvider{
		values: map[string]string{
			"bw:myserver/password": "s3cret",
		},
	})
	got, err := r.ResolveValue("!bw:myserver/password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("got %q, want %q", got, "s3cret")
	}
}

func TestResolveValue_OnePasswordPrefix(t *testing.T) {
	r := NewResolver()
	r.Register("op", &mockProvider{
		values: map[string]string{
			"op://vault/item/password": "op-secret",
		},
	})
	got, err := r.ResolveValue("!op://vault/item/password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "op-secret" {
		t.Errorf("got %q, want %q", got, "op-secret")
	}
}

func TestResolveValue_UnregisteredProvider(t *testing.T) {
	r := NewResolver()
	// Don't register the "bw" provider
	_, err := r.ResolveValue("!bw:myserver/password")
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}

func TestResolveMap_Recursive(t *testing.T) {
	r := NewResolver()
	r.Register("bw", &mockProvider{
		values: map[string]string{
			"bw:item/pass":     "resolved_pass",
			"bw:item/username": "resolved_user",
		},
	})

	input := map[string]interface{}{
		"top_level": "!bw:item/pass",
		"nested": map[string]interface{}{
			"inner": "!bw:item/username",
			"plain": "no-change",
		},
	}

	got, err := r.ResolveMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["top_level"] != "resolved_pass" {
		t.Errorf("top_level = %v, want %q", got["top_level"], "resolved_pass")
	}
	nested := got["nested"].(map[string]interface{})
	if nested["inner"] != "resolved_user" {
		t.Errorf("nested.inner = %v, want %q", nested["inner"], "resolved_user")
	}
	if nested["plain"] != "no-change" {
		t.Errorf("nested.plain = %v, want %q", nested["plain"], "no-change")
	}
}

func TestResolveMap_MixedValues(t *testing.T) {
	r := NewResolver()
	r.Register("bw", &mockProvider{
		values: map[string]string{"bw:item/pass": "secret"},
	})

	input := map[string]interface{}{
		"secret_val": "!bw:item/pass",
		"plain_str":  "hello",
		"int_val":    42,
		"bool_val":   true,
		"float_val":  3.14,
		"list_val":   []interface{}{"a", "!bw:item/pass", 3},
	}

	got, err := r.ResolveMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["secret_val"] != "secret" {
		t.Errorf("secret_val = %v, want %q", got["secret_val"], "secret")
	}
	if got["plain_str"] != "hello" {
		t.Errorf("plain_str changed unexpectedly")
	}
	if got["int_val"] != 42 {
		t.Errorf("int_val changed unexpectedly")
	}
	if got["bool_val"] != true {
		t.Errorf("bool_val changed unexpectedly")
	}
	list := got["list_val"].([]interface{})
	if list[1] != "secret" {
		t.Errorf("list_val[1] = %v, want %q", list[1], "secret")
	}
}

func TestResolveMap_EmptyMap(t *testing.T) {
	r := NewResolver()
	got, err := r.ResolveMap(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestResolveMap_NilMap(t *testing.T) {
	r := NewResolver()
	got, err := r.ResolveMap(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestResolveMap_ErrorPropagation(t *testing.T) {
	r := NewResolver()
	r.Register("bw", &mockProvider{
		err: fmt.Errorf("vault locked"),
	})

	input := map[string]interface{}{
		"pass": "!bw:item/password",
	}
	_, err := r.ResolveMap(input)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}
