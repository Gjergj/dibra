package docker_compose

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// Action keywords used to detect changes in compose output
var actionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(creat(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(start(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(recreat(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(remov(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(stopp(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(kill(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(pull(ed|ing))\b`),
	regexp.MustCompile(`(?i)\b(build(ing)?)\b`),
	regexp.MustCompile(`(?i)\b(restart(ed|ing))\b`),
}

func Execute(req Request) Response {
	// Check if directory exists
	if _, err := os.Stat(req.ProjectSrc); os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("project_src does not exist: %s", req.ProjectSrc)}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	// Construct environment
	cmdEnv := docker.GetComposeEnv(req.ComposeCommonArgs, req.CommonArgs)

	// Base args with --ansi never for stable output parsing
	args := []string{"compose", "--ansi", "never"}
	args = append(args, docker.GetComposeBaseArgs(req.ComposeCommonArgs)[1:]...) // skip "compose" already added

	// Use cmd.Dir for project directory context
	runDir := req.ProjectSrc

	// Main action
	var actionArgs []string

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
		if req.NoDeps {
			actionArgs = append(actionArgs, "--no-deps")
		}
		if req.Wait {
			actionArgs = append(actionArgs, "--wait")
		}
		if req.WaitTimeout > 0 {
			actionArgs = append(actionArgs, "--wait-timeout", fmt.Sprintf("%d", req.WaitTimeout))
		}

		// Handle recreate parameter
		recreate := req.Recreate
		// Backward compat: honor deprecated flags
		if req.ForceRecreate && recreate == "" {
			recreate = "always"
		}
		if req.NoRecreate && recreate == "" {
			recreate = "never"
		}

		switch recreate {
		case "always":
			actionArgs = append(actionArgs, "--force-recreate")
		case "never":
			actionArgs = append(actionArgs, "--no-recreate")
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
		if req.RemoveVolumes {
			actionArgs = append(actionArgs, "--volumes")
		}
		if req.RemoveImages != "" {
			actionArgs = append(actionArgs, "--rmi", req.RemoveImages)
		}
		if req.StopTimeout > 0 {
			actionArgs = append(actionArgs, "--timeout", fmt.Sprintf("%d", req.StopTimeout))
		}
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

	// Detect changes using improved heuristics
	changed, actions := detectChanges(outputStr)

	return Response{
		Changed: changed,
		Msg:     "command executed",
		Stdout:  outputStr,
		Actions: actions,
	}
}

// detectChanges analyzes compose output to determine if changes were made
func detectChanges(output string) (bool, []string) {
	var actions []string
	actionSet := make(map[string]bool)

	for _, pattern := range actionPatterns {
		matches := pattern.FindAllString(output, -1)
		for _, match := range matches {
			action := strings.ToLower(match)
			if !actionSet[action] {
				actionSet[action] = true
				actions = append(actions, action)
			}
		}
	}

	// Additional heuristics: check for "Container ... Running" (no change)
	// vs "Container ... Created" (change)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		// Skip "Running" status lines (indicates no change for that container)
		if strings.Contains(lower, "running") && !strings.Contains(lower, "creat") && !strings.Contains(lower, "start") {
			continue
		}
	}

	return len(actions) > 0, actions
}
