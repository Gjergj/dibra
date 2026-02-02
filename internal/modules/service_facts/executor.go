package service_facts

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const systemctlCmd = "/usr/bin/systemctl"

func Execute(req Request) Response {
	services := make(map[string]ServiceInfo)

	if isSystemdManaged() {
		gatherSystemdServices(services)
	}

	gatherSysVInitServices(services)

	return Response{
		Changed:  false,
		Services: services,
	}
}

func isSystemdManaged() bool {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	if _, err := os.Stat(systemctlCmd); err == nil {
		return true
	}
	return false
}

func gatherSystemdServices(services map[string]ServiceInfo) {
	gatherFromUnits(services)
	gatherFromUnitFiles(services)
}

func gatherFromUnits(services map[string]ServiceInfo) {
	cmd := exec.Command(systemctlCmd, "list-units", "--no-pager", "--type", "service", "--all", "--plain")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return
	}

	badStates := map[string]bool{
		"not-found": true,
		"masked":    true,
		"failed":    true,
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		if !strings.Contains(line, ".service") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		serviceName := fields[0]
		stateVal := "stopped"
		statusVal := "unknown"

		for i := 1; i < len(fields)-1; i++ {
			if badStates[fields[i]] {
				statusVal = fields[i]
				break
			}
		}

		if statusVal == "unknown" && len(fields) > 2 {
			statusVal = fields[2]
		}

		if len(fields) > 3 && fields[3] == "running" {
			stateVal = "running"
		}

		services[serviceName] = ServiceInfo{
			Name:   serviceName,
			State:  stateVal,
			Status: statusVal,
			Source: "systemd",
		}
	}
}

func gatherFromUnitFiles(services map[string]ServiceInfo) {
	cmd := exec.Command(systemctlCmd, "list-unit-files", "--no-pager", "--type", "service", "--all")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return
	}

	badStates := map[string]bool{
		"not-found": true,
		"masked":    true,
		"failed":    true,
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		if !strings.Contains(line, ".service") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		serviceName := fields[0]
		statusVal := fields[1]

		if existing, ok := services[serviceName]; ok {
			if !badStates[existing.Status] {
				existing.Status = statusVal
				services[serviceName] = existing
			}
		} else {
			state := getActiveState(serviceName)
			services[serviceName] = ServiceInfo{
				Name:   serviceName,
				State:  state,
				Status: statusVal,
				Source: "systemd",
			}
		}
	}
}

func getActiveState(unitName string) string {
	cmd := exec.Command(systemctlCmd, "show", unitName, "--property=ActiveState")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "unknown"
	}

	output := strings.TrimSpace(stdout.String())
	if strings.HasPrefix(output, "ActiveState=") {
		state := strings.TrimPrefix(output, "ActiveState=")
		switch state {
		case "active", "activating":
			return "running"
		case "inactive", "deactivating":
			return "stopped"
		case "failed":
			return "failed"
		default:
			return state
		}
	}
	return "unknown"
}

func gatherSysVInitServices(services map[string]ServiceInfo) {
	servicePath, err := exec.LookPath("service")
	if err != nil {
		return
	}

	cmd := exec.Command(servicePath, "--status-all")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderr2 := stderr.String()
		stdout2 := stdout.String()
		if !strings.Contains(stderr2, "") && !strings.Contains(stdout2, "[") {
			return
		}
	}

	combined := stdout.String() + stderr.String()

	pattern := regexp.MustCompile(`^\s*\[\s*([+-?])\s*\]\s+(.+)$`)

	for _, line := range strings.Split(combined, "\n") {
		matches := pattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		stateChar := matches[1]
		serviceName := strings.TrimSpace(matches[2])

		if _, exists := services[serviceName+".service"]; exists {
			continue
		}

		var serviceState string
		switch stateChar {
		case "+":
			serviceState = "running"
		case "-":
			serviceState = "stopped"
		default:
			serviceState = "unknown"
		}

		services[serviceName] = ServiceInfo{
			Name:   serviceName,
			State:  serviceState,
			Source: "sysv",
		}
	}
}
