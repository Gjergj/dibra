// Package execution defines the controller-to-agent module invocation protocol.
package execution

import "fmt"

// State is the effective execution state for one task. Task-level values are
// resolved against the global controller state before the request is sent.
type State struct {
	CheckMode bool `json:"check_mode"`
	DiffMode  bool `json:"diff"`
}

// ModuleRequest is the stable controller-to-agent invocation envelope. T is
// interface{} in the controller and json.RawMessage in the agent.
type ModuleRequest[T any] struct {
	Module string `json:"module"`
	Args   T      `json:"args"`
	State
}

// SkippedResult is returned without invoking a module when check mode is not
// implemented. Keeping this decision at the execution boundary prevents a
// nominal dry run from changing the target.
type SkippedResult struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Skipped bool   `json:"skipped"`
	Msg     string `json:"msg"`
}

// UnsupportedCheckMode returns the standard non-mutating result for a module
// that has not opted into Dibra's check-mode execution contract.
func UnsupportedCheckMode(module string) SkippedResult {
	return SkippedResult{
		Skipped: true,
		Msg:     fmt.Sprintf("%s does not yet implement check mode in Dibra", module),
	}
}

// ResolveState applies optional task-level overrides to global execution
// state. A nil override inherits the corresponding global value.
func ResolveState(global State, taskCheckMode, taskDiffMode *bool) State {
	resolved := global
	if taskCheckMode != nil {
		resolved.CheckMode = *taskCheckMode
	}
	if taskDiffMode != nil {
		resolved.DiffMode = *taskDiffMode
	}
	return resolved
}
