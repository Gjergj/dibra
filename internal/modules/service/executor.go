package service

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ServiceManager interface {
	GetStatus(name string) (map[string]string, error)
	IsRunning(name string) bool
	IsEnabled(name string) bool
	Start(name string, args string) error
	Stop(name string) error
	Restart(name string, args string, sleep int) error
	Reload(name string) error
	Enable(name string) error
	Disable(name string) error
	ServiceExists(name string) bool
}

func Execute(req Request) Response {
	if req.Name == "" {
		return Response{
			Failed: true,
			Msg:    "name is required",
		}
	}

	if req.State == "" && req.Enabled == nil {
		return Response{
			Failed: true,
			Msg:    "at least one of state or enabled is required",
		}
	}

	if req.State != "" {
		validStates := map[string]bool{
			"started":   true,
			"stopped":   true,
			"restarted": true,
			"reloaded":  true,
		}
		if !validStates[req.State] {
			return Response{
				Failed: true,
				Msg:    "invalid state: " + req.State + " (must be started, stopped, restarted, or reloaded)",
			}
		}
	}

	mgr := detectServiceManager(req.Use)

	resp := Response{
		Name: req.Name,
	}

	if !mgr.ServiceExists(req.Name) {
		if req.Pattern != "" {
			if isProcessRunning(req.Pattern) {
				resp.State = "started"
			}
		} else {
			return Response{
				Failed: true,
				Msg:    "Could not find the requested service " + req.Name,
			}
		}
	}

	status, _ := mgr.GetStatus(req.Name)
	resp.Status = status

	if req.Enabled != nil {
		currentlyEnabled := mgr.IsEnabled(req.Name)
		if *req.Enabled && !currentlyEnabled {
			if err := mgr.Enable(req.Name); err != nil {
				return Response{
					Failed: true,
					Msg:    "failed to enable service: " + err.Error(),
				}
			}
			resp.Changed = true
			resp.Enabled = true
		} else if !*req.Enabled && currentlyEnabled {
			if err := mgr.Disable(req.Name); err != nil {
				return Response{
					Failed: true,
					Msg:    "failed to disable service: " + err.Error(),
				}
			}
			resp.Changed = true
			resp.Enabled = false
		} else {
			resp.Enabled = currentlyEnabled
		}
	}

	if req.State != "" {
		currentlyRunning := mgr.IsRunning(req.Name)
		if !currentlyRunning && req.Pattern != "" {
			currentlyRunning = isProcessRunning(req.Pattern)
		}

		switch req.State {
		case "started":
			resp.State = "started"
			if !currentlyRunning {
				if err := mgr.Start(req.Name, req.Arguments); err != nil {
					return Response{
						Failed: true,
						Msg:    "failed to start service: " + err.Error(),
					}
				}
				resp.Changed = true
			}

		case "stopped":
			resp.State = "stopped"
			if currentlyRunning {
				if err := mgr.Stop(req.Name); err != nil {
					return Response{
						Failed: true,
						Msg:    "failed to stop service: " + err.Error(),
					}
				}
				resp.Changed = true
			}

		case "restarted":
			resp.State = "started"
			if err := mgr.Restart(req.Name, req.Arguments, req.Sleep); err != nil {
				return Response{
					Failed: true,
					Msg:    "failed to restart service: " + err.Error(),
				}
			}
			resp.Changed = true

		case "reloaded":
			resp.State = "started"
			if !currentlyRunning {
				if err := mgr.Start(req.Name, req.Arguments); err != nil {
					return Response{
						Failed: true,
						Msg:    "failed to start service before reload: " + err.Error(),
					}
				}
				resp.Changed = true
			}
			if err := mgr.Reload(req.Name); err != nil {
				return Response{
					Failed: true,
					Msg:    "failed to reload service: " + err.Error(),
				}
			}
			resp.Changed = true
		}
	}

	newStatus, _ := mgr.GetStatus(req.Name)
	resp.Status = newStatus

	return resp
}

func detectServiceManager(use string) ServiceManager {
	if use != "" && use != "auto" {
		switch use {
		case "systemd":
			return &systemdManager{}
		case "sysvinit", "sysv":
			return &sysvinitManager{}
		}
	}

	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return &systemdManager{}
	}

	if _, err := os.Stat("/etc/init.d"); err == nil {
		return &sysvinitManager{}
	}

	return &systemdManager{}
}

func isProcessRunning(pattern string) bool {
	cmd := exec.Command("pgrep", "-f", pattern)
	err := cmd.Run()
	return err == nil
}

type systemdManager struct{}

func (m *systemdManager) runSystemctl(args ...string) (int, string, string) {
	cmd := exec.Command("/usr/bin/systemctl", args...)
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

func (m *systemdManager) normalizeUnitName(name string) string {
	if !strings.Contains(name, ".") {
		return name + ".service"
	}
	return name
}

func (m *systemdManager) GetStatus(name string) (map[string]string, error) {
	unitName := m.normalizeUnitName(name)
	rc, stdout, _ := m.runSystemctl("show", unitName)
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

func (m *systemdManager) IsRunning(name string) bool {
	unitName := m.normalizeUnitName(name)
	rc, stdout, _ := m.runSystemctl("is-active", unitName)
	if rc == 0 {
		return true
	}
	trimmed := strings.TrimSpace(stdout)
	return trimmed == "active" || trimmed == "activating"
}

func (m *systemdManager) IsEnabled(name string) bool {
	unitName := m.normalizeUnitName(name)
	rc, stdout, _ := m.runSystemctl("is-enabled", unitName)
	trimmed := strings.TrimSpace(stdout)
	if rc == 0 {
		return trimmed == "enabled" || trimmed == "enabled-runtime" || trimmed == "static"
	}
	return trimmed == "enabled" || trimmed == "enabled-runtime" || trimmed == "static"
}

func (m *systemdManager) Start(name string, args string) error {
	unitName := m.normalizeUnitName(name)
	rc, _, stderr := m.runSystemctl("start", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) Stop(name string) error {
	unitName := m.normalizeUnitName(name)
	rc, _, stderr := m.runSystemctl("stop", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) Restart(name string, args string, sleep int) error {
	unitName := m.normalizeUnitName(name)
	if sleep > 0 {
		if err := m.Stop(name); err != nil {
			return err
		}
		time.Sleep(time.Duration(sleep) * time.Second)
		return m.Start(name, args)
	}
	rc, _, stderr := m.runSystemctl("restart", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) Reload(name string) error {
	unitName := m.normalizeUnitName(name)
	rc, _, stderr := m.runSystemctl("reload", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) Enable(name string) error {
	unitName := m.normalizeUnitName(name)
	rc, _, stderr := m.runSystemctl("enable", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) Disable(name string) error {
	unitName := m.normalizeUnitName(name)
	rc, _, stderr := m.runSystemctl("disable", unitName)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *systemdManager) ServiceExists(name string) bool {
	unitName := m.normalizeUnitName(name)
	rc, stdout, _ := m.runSystemctl("show", unitName)
	if rc != 0 {
		return false
	}

	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "LoadState=") {
			loadState := strings.TrimPrefix(line, "LoadState=")
			return loadState != "not-found"
		}
	}

	_, stdout, _ = m.runSystemctl("is-enabled", unitName)
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

type sysvinitManager struct{}

func (m *sysvinitManager) runService(args ...string) (int, string, string) {
	cmd := exec.Command("/usr/sbin/service", args...)
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

func (m *sysvinitManager) GetStatus(name string) (map[string]string, error) {
	rc, stdout, _ := m.runService(name, "status")
	status := make(map[string]string)
	status["stdout"] = stdout
	if rc == 0 {
		status["running"] = "true"
	} else {
		status["running"] = "false"
	}
	return status, nil
}

func (m *sysvinitManager) IsRunning(name string) bool {
	rc, _, _ := m.runService(name, "status")
	return rc == 0
}

func (m *sysvinitManager) IsEnabled(name string) bool {
	initScript := "/etc/init.d/" + name
	if _, err := os.Stat(initScript); os.IsNotExist(err) {
		return false
	}

	if output, err := exec.Command("update-rc.d", "-n", name, "defaults").CombinedOutput(); err == nil {
		if strings.Contains(string(output), "already exist") {
			return true
		}
	}

	for _, runlevel := range []string{"2", "3", "4", "5"} {
		rcDir := "/etc/rc" + runlevel + ".d"
		if files, err := os.ReadDir(rcDir); err == nil {
			for _, f := range files {
				if strings.HasPrefix(f.Name(), "S") && strings.Contains(f.Name(), name) {
					return true
				}
			}
		}
	}

	return false
}

func (m *sysvinitManager) Start(name string, args string) error {
	cmdArgs := []string{name, "start"}
	if args != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	rc, _, stderr := m.runService(cmdArgs...)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *sysvinitManager) Stop(name string) error {
	rc, _, stderr := m.runService(name, "stop")
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *sysvinitManager) Restart(name string, args string, sleep int) error {
	if sleep > 0 {
		if err := m.Stop(name); err != nil {
			return err
		}
		time.Sleep(time.Duration(sleep) * time.Second)
		return m.Start(name, args)
	}
	cmdArgs := []string{name, "restart"}
	if args != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	rc, _, stderr := m.runService(cmdArgs...)
	if rc != 0 {
		return &serviceError{msg: stderr}
	}
	return nil
}

func (m *sysvinitManager) Reload(name string) error {
	rc, _, _ := m.runService(name, "reload")
	if rc != 0 {
		var stderr string
		rc, _, stderr = m.runService(name, "force-reload")
		if rc != 0 {
			return &serviceError{msg: stderr}
		}
	}
	return nil
}

func (m *sysvinitManager) Enable(name string) error {
	cmd := exec.Command("update-rc.d", name, "defaults")
	if output, err := cmd.CombinedOutput(); err != nil {
		return &serviceError{msg: string(output)}
	}
	return nil
}

func (m *sysvinitManager) Disable(name string) error {
	cmd := exec.Command("update-rc.d", "-f", name, "remove")
	if output, err := cmd.CombinedOutput(); err != nil {
		return &serviceError{msg: string(output)}
	}
	return nil
}

func (m *sysvinitManager) ServiceExists(name string) bool {
	initScript := "/etc/init.d/" + name
	_, err := os.Stat(initScript)
	return err == nil
}

type serviceError struct {
	msg string
}

func (e *serviceError) Error() string {
	return strings.TrimSpace(e.msg)
}

func init() {
	_ = strconv.Itoa(0)
}
