package execution

import (
	"encoding/json"
	"testing"
)

func TestResolveStateAppliesTaskOverridesIndependently(t *testing.T) {
	falseValue := false
	trueValue := true

	tests := []struct {
		name      string
		global    State
		taskCheck *bool
		taskDiff  *bool
		want      State
	}{
		{name: "inherits global state", global: State{CheckMode: true}, want: State{CheckMode: true}},
		{name: "task disables global check", global: State{CheckMode: true}, taskCheck: &falseValue, want: State{}},
		{name: "task enables diff", taskDiff: &trueValue, want: State{DiffMode: true}},
		{name: "overrides are independent", global: State{CheckMode: true, DiffMode: true}, taskDiff: &falseValue, want: State{CheckMode: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveState(test.global, test.taskCheck, test.taskDiff); got != test.want {
				t.Fatalf("ResolveState() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestModuleRequestAlwaysCarriesExecutionState(t *testing.T) {
	data, err := json.Marshal(ModuleRequest[map[string]any]{
		Module: "ping",
		Args:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"module", "args", "check_mode", "diff"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("invocation envelope does not contain %q: %s", field, data)
		}
	}
}

func TestUnsupportedCheckModeIsAnUnchangedSkip(t *testing.T) {
	result := UnsupportedCheckMode("example.module")
	if result.Changed || result.Failed || !result.Skipped {
		t.Fatalf("UnsupportedCheckMode() = %#v", result)
	}
	if result.Msg != "example.module does not yet implement check mode in Dibra" {
		t.Fatalf("message = %q", result.Msg)
	}
}
