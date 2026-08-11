// Package execution defines the controller-to-agent module invocation protocol.
package execution

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
