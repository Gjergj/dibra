package docker

import (
	"encoding/json"
	"testing"
)

func TestPythonStringMatchesDockerOptionConversion(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{true, "true"},
		{false, "false"},
		{nil, "None"},
		{json.Number("42"), "42"},
		{[]any{"one", true, nil}, "['one', True, None]"},
		{map[string]any{"enabled": true, "value": "x"}, "{'enabled': True, 'value': 'x'}"},
	}
	for _, test := range tests {
		if got := PythonString(test.value); got != test.want {
			t.Fatalf("PythonString(%#v) = %q, want %q", test.value, got, test.want)
		}
	}
	if got := PythonRepr(true); got != "True" {
		t.Fatalf("PythonRepr(true) = %q, want True", got)
	}
	if got := PythonToText(true); got != "True" {
		t.Fatalf("PythonToText(true) = %q, want True", got)
	}
	if got := PythonToText("plain"); got != "plain" {
		t.Fatalf("PythonToText(plain) = %q, want plain", got)
	}
}
