package docker_compose

import (
	"context"
	"fmt"
	"os"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	// Check if directory exists
	if _, err := dependencies.FileSystem.Stat(req.ProjectSrc); os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("project_src does not exist: %s", req.ProjectSrc)}
	}
	if _, err := docker.CheckComposeVersion(context.Background(), dependencies.CLIRunner, req.CommonArgs, dependencies.Environment); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	// Construct environment
	cmdEnv, err := docker.GetComposeEnvWithEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid Docker connection options: %v", err)}
	}

	// Compose 5.4 provides stable machine-readable progress events.
	args, err := docker.GetComposeBaseArgsWithEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid Docker connection options: %v", err)}
	}
	args = append(args, "--ansi", "never", "--progress", "json")

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

	result, err := dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name: "docker",
		Args: finalArgs,
		Dir:  runDir,
		Env:  cmdEnv,
	})
	outputStr := string(result.Output)

	if err != nil {
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("command failed: %v", err),
			Stdout: outputStr,
		}
	}

	events := docker.ParseComposeJSONEvents(result.Output)
	changed := docker.ComposeEventsChanged(events.Events)
	actions := docker.ComposeEventActions(events.Events)

	return Response{
		Changed: changed,
		Msg:     "command executed",
		Stdout:  outputStr,
		Actions: actions,
	}
}
