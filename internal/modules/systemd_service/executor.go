package systemd_service

import (
	"bytes"
	"os/exec"
	"strings"
)

const systemctlCmd = "/usr/bin/systemctl"

func Execute(req Request) Response {
	if req.Scope == "" {
		req.Scope = "system"
	}

	if req.Name != "" {
		for _, pattern := range []string{"*", "?", "["} {
			if strings.Contains(req.Name, pattern) {
				return Response{
					Failed: true,
					Msg:    "glob patterns are not supported in service name",
				}
			}
		}
	}

	if req.State == "" && req.Enabled == nil && req.Masked == nil && !req.DaemonReload && !req.DaemonReexec {
		return Response{
			Failed: true,
			Msg:    "at least one of state, enabled, masked, daemon_reload, or daemon_reexec is required",
		}
	}

	if (req.State != "" || req.Enabled != nil || req.Masked != nil) && req.Name == "" {
		return Response{
			Failed: true,
			Msg:    "name is required when state, enabled, or masked is set",
		}
	}

	resp := Response{
		Name: req.Name,
	}

	if req.DaemonReexec {
		rc, _, stderr := runSystemctl(req.Scope, "daemon-reexec")
		if rc != 0 {
			return Response{
				Failed: true,
				Msg:    "daemon-reexec failed: " + stderr,
			}
		}
	}

	if req.DaemonReload {
		rc, _, stderr := runSystemctl(req.Scope, "daemon-reload")
		if rc != 0 {
			return Response{
				Failed: true,
				Msg:    "daemon-reload failed: " + stderr,
			}
		}
	}

	if req.Name == "" {
		resp.Changed = req.DaemonReload || req.DaemonReexec
		if req.DaemonReload && req.DaemonReexec {
			resp.Msg = "daemon-reexec and daemon-reload executed"
		} else if req.DaemonReload {
			resp.Msg = "daemon-reload executed"
		} else if req.DaemonReexec {
			resp.Msg = "daemon-reexec executed"
		}
		return resp
	}

	unitName := normalizeUnitName(req.Name)

	status, err := getUnitStatus(req.Scope, unitName)
	if err != nil {
		return Response{
			Failed: true,
			Msg:    "failed to get unit status: " + err.Error(),
		}
	}
	resp.Status = status

	if !unitExists(req.Scope, unitName) && req.Masked == nil {
		return Response{
			Failed: true,
			Msg:    "could not find the requested service " + unitName,
		}
	}

	if req.Masked != nil {
		currentlyMasked := isUnitMasked(req.Scope, unitName)
		if *req.Masked && !currentlyMasked {
			rc, _, stderr := runSystemctl(req.Scope, "mask", unitName)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to mask service: " + stderr,
				}
			}
			resp.Changed = true
		} else if !*req.Masked && currentlyMasked {
			rc, _, stderr := runSystemctl(req.Scope, "unmask", unitName)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to unmask service: " + stderr,
				}
			}
			resp.Changed = true
		}
	}

	if req.Enabled != nil {
		currentlyEnabled := isUnitEnabled(req.Scope, unitName)
		if *req.Enabled && !currentlyEnabled {
			args := []string{"enable"}
			if req.Force {
				args = append(args, "--force")
			}
			args = append(args, unitName)
			rc, _, stderr := runSystemctl(req.Scope, args...)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to enable service: " + stderr,
				}
			}
			resp.Changed = true
			resp.Enabled = true
		} else if !*req.Enabled && currentlyEnabled {
			rc, _, stderr := runSystemctl(req.Scope, "disable", unitName)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to disable service: " + stderr,
				}
			}
			resp.Changed = true
			resp.Enabled = false
		} else {
			resp.Enabled = currentlyEnabled
		}
	}

	if req.State != "" {
		currentlyRunning := isUnitRunning(status)
		currentlyDeactivating := isUnitDeactivating(status)

		switch req.State {
		case "started":
			resp.State = "started"
			if !currentlyRunning {
				args := []string{"start"}
				if req.NoBlock {
					args = append(args, "--no-block")
				}
				args = append(args, unitName)
				rc, _, stderr := runSystemctl(req.Scope, args...)
				if rc != 0 {
					return Response{
						Failed: true,
						Msg:    "failed to start service: " + stderr,
					}
				}
				resp.Changed = true
			}

		case "stopped":
			resp.State = "stopped"
			if currentlyRunning || currentlyDeactivating {
				args := []string{"stop"}
				if req.NoBlock {
					args = append(args, "--no-block")
				}
				args = append(args, unitName)
				rc, _, stderr := runSystemctl(req.Scope, args...)
				if rc != 0 {
					return Response{
						Failed: true,
						Msg:    "failed to stop service: " + stderr,
					}
				}
				resp.Changed = true
			}

		case "restarted":
			resp.State = "started"
			args := []string{"restart"}
			if req.NoBlock {
				args = append(args, "--no-block")
			}
			args = append(args, unitName)
			rc, _, stderr := runSystemctl(req.Scope, args...)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to restart service: " + stderr,
				}
			}
			resp.Changed = true

		case "reloaded":
			resp.State = "started"
			if !currentlyRunning {
				args := []string{"start"}
				if req.NoBlock {
					args = append(args, "--no-block")
				}
				args = append(args, unitName)
				rc, _, stderr := runSystemctl(req.Scope, args...)
				if rc != 0 {
					return Response{
						Failed: true,
						Msg:    "failed to start service before reload: " + stderr,
					}
				}
				resp.Changed = true
			}
			args := []string{"reload"}
			if req.NoBlock {
				args = append(args, "--no-block")
			}
			args = append(args, unitName)
			rc, _, stderr := runSystemctl(req.Scope, args...)
			if rc != 0 {
				return Response{
					Failed: true,
					Msg:    "failed to reload service: " + stderr,
				}
			}
			resp.Changed = true

		default:
			return Response{
				Failed: true,
				Msg:    "invalid state: " + req.State + " (must be started, stopped, restarted, or reloaded)",
			}
		}
	}

	newStatus, _ := getUnitStatus(req.Scope, unitName)
	resp.Status = newStatus

	return resp
}

func normalizeUnitName(name string) string {
	if !strings.Contains(name, ".") {
		return name + ".service"
	}
	return name
}

func runSystemctl(scope string, args ...string) (int, string, string) {
	fullArgs := []string{}
	switch scope {
	case "user":
		fullArgs = append(fullArgs, "--user")
	case "global":
		fullArgs = append(fullArgs, "--global")
	}
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command(systemctlCmd, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	rc := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else {
			rc = 1
		}
	}

	return rc, stdout.String(), stderr.String()
}

func getUnitStatus(scope, unitName string) (map[string]string, error) {
	rc, stdout, _ := runSystemctl(scope, "show", unitName)
	if rc != 0 {
		return nil, nil
	}

	status := make(map[string]string)
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			status[parts[0]] = parts[1]
		}
	}
	return status, nil
}

func unitExists(scope, unitName string) bool {
	rc, stdout, _ := runSystemctl(scope, "show", unitName)
	if rc != 0 {
		return false
	}

	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "LoadState=") {
			loadState := strings.TrimPrefix(line, "LoadState=")
			return loadState != "not-found"
		}
	}

	_, stdout, _ = runSystemctl(scope, "is-enabled", unitName)
	validStates := []string{
		"enabled", "enabled-runtime", "linked", "linked-runtime",
		"masked", "masked-runtime", "static", "indirect",
		"disabled", "generated", "transient",
	}
	trimmed := strings.TrimSpace(stdout)
	for _, state := range validStates {
		if trimmed == state {
			return true
		}
	}

	return false
}

func isUnitRunning(status map[string]string) bool {
	if status == nil {
		return false
	}
	activeState := status["ActiveState"]
	return activeState == "active" || activeState == "activating"
}

func isUnitDeactivating(status map[string]string) bool {
	if status == nil {
		return false
	}
	return status["ActiveState"] == "deactivating"
}

func isUnitEnabled(scope, unitName string) bool {
	rc, stdout, _ := runSystemctl(scope, "is-enabled", unitName)
	if rc != 0 {
		trimmed := strings.TrimSpace(stdout)
		return trimmed == "enabled" || trimmed == "enabled-runtime" || trimmed == "static"
	}
	trimmed := strings.TrimSpace(stdout)
	return trimmed == "enabled" || trimmed == "enabled-runtime" || trimmed == "static"
}

func isUnitMasked(scope, unitName string) bool {
	rc, stdout, _ := runSystemctl(scope, "is-enabled", unitName)
	if rc != 0 {
		trimmed := strings.TrimSpace(stdout)
		return trimmed == "masked" || trimmed == "masked-runtime"
	}
	trimmed := strings.TrimSpace(stdout)
	return trimmed == "masked" || trimmed == "masked-runtime"
}
