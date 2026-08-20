package command

import (
	"reflect"
	"testing"
)

func TestExecuteCoreParityValidationAndResultFields(t *testing.T) {
	missing := Execute(Request{})
	if !missing.Failed || missing.Msg != "one of cmd or argv is required" || missing.Changed {
		t.Fatalf("missing command = %#v", missing)
	}

	result := Execute(Request{
		Argv: []string{"/bin/sh", "-c", "printf unit-out; printf unit-err >&2; exit 7"},
	})
	if !result.Failed ||
		!result.Changed ||
		result.RC != 7 ||
		result.Stdout != "unit-out" ||
		result.Stderr != "unit-err" ||
		!reflect.DeepEqual(result.Cmd, []string{"/bin/sh", "-c", "printf unit-out; printf unit-err >&2; exit 7"}) {
		t.Fatalf("nonzero result = %#v", result)
	}
	if result.Start == "" || result.End == "" || result.Delta == "" {
		t.Fatalf("timing fields were not populated: %#v", result)
	}
}
