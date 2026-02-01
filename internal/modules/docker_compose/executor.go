package docker_compose

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Execute(req Request) Response {
	// docker_compose typically relies on the CLI (docker compose or docker-compose)
	// We will try 'docker compose' (v2 plugin) first.

	// Check if directory exists
	if _, err := os.Stat(req.ProjectSrc); os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("project_src does not exist: %s", req.ProjectSrc)}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	// Construct command override
	// We might need to select context, but CommonArgs handles client connection, not necessarily CLI context.
	// For CLI wrapping, we must rely on DOCKER_HOST env var being passed to the process.

	cmdEnv := os.Environ()

	// Apply connection args to Env
	if req.DockerHost != "" {
		cmdEnv = append(cmdEnv, fmt.Sprintf("DOCKER_HOST=%s", req.DockerHost))
	}
	if req.TLS {
		cmdEnv = append(cmdEnv, "DOCKER_TLS_VERIFY=1")
		if req.CAPath != "" {
			cmdEnv = append(cmdEnv, fmt.Sprintf("DOCKER_CERT_PATH=%s", req.CAPath))
		}
	}
	// Add user envs
	for k, v := range req.Env {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	// COMPOSE_PROFILES
	if len(req.Profiles) > 0 {
		cmdEnv = append(cmdEnv, fmt.Sprintf("COMPOSE_PROFILES=%s", strings.Join(req.Profiles, ",")))
	}

	// Base args
	args := []string{"compose"}

	if req.ProjectName != "" {
		args = append(args, "--project-name", req.ProjectName)
	}

	// Use cmd.Dir for project directory context
	runDir := req.ProjectSrc
	// If files are provided, they are relative to runDir (or absolute)
	for _, f := range req.Files {
		args = append(args, "--file", f)
	}

	// Main action
	var actionArgs []string

	// Check idempotency?
	// It's hard with CLI. Usually we just run 'up -d' which is idempotent-ish (recreates if changed).
	// To report "changed", we might need to parse output or check state before.
	// For now, we'll mark changed=true if run succeeds, unless we can detect "Up to date".

	if state == "present" {
		actionArgs = []string{"up", "-d"}
		if req.Build {
			actionArgs = append(actionArgs, "--build")
		}
		if req.Pull {
			actionArgs = append(actionArgs, "--pull", "always")
		}
		if req.RemoveOrphans {
			actionArgs = append(actionArgs, "--remove-orphans")
		}
		// Scale
		for svc, n := range req.Scale {
			actionArgs = append(actionArgs, "--scale", fmt.Sprintf("%s=%d", svc, n))
		}
		// Services
		if len(req.Services) > 0 {
			actionArgs = append(actionArgs, req.Services...)
		}
	} else if state == "absent" {
		actionArgs = []string{"down"}
		if req.RemoveOrphans {
			actionArgs = append(actionArgs, "--remove-orphans")
		}
		// docker compose down doesn't take service names usually, it takes down the whole stack.
		// If services are specified, 'down' might not work as expected via CLI the same way?
		// Actually 'docker compose down' removes the whole project resources.
	} else {
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
	}

	finalArgs := append(args, actionArgs...)

	cmd := exec.Command("docker", finalArgs...)
	cmd.Dir = runDir
	cmd.Env = cmdEnv

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("command failed: %v", err),
			Stdout: outputStr,
		}
	}

	// Detect changes?
	// If output contains "Created", "Started", "Recreated", "Removed", "Stopping", "Killing"...
	changed := false
	lowerOut := strings.ToLower(outputStr)
	keywords := []string{"created", "started", "recreated", "removed", "stopping", "killing", "pulled", "building"}
	for _, k := range keywords {
		if strings.Contains(lowerOut, k) {
			changed = true
			break
		}
	}

	// If "up -d" runs on already running containers, it usually says "Running" or "Up-to-date" (actually silence or "Container ... Running").
	// If output is empty or very short, might be no change.

	return Response{
		Changed: changed,
		Msg:     "command executed",
		Stdout:  outputStr,
	}
}
