package docker_stack

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Execute(req Request) Response {
	// docker_stack wraps the CLI: docker stack deploy / docker stack rm

	if req.Name == "" {
		return Response{Failed: true, Msg: "name is required"}
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	cmdEnv := os.Environ()
	if req.DockerHost != "" {
		cmdEnv = append(cmdEnv, fmt.Sprintf("DOCKER_HOST=%s", req.DockerHost))
	}
	if req.TLS {
		cmdEnv = append(cmdEnv, "DOCKER_TLS_VERIFY=1")
		if req.CAPath != "" {
			cmdEnv = append(cmdEnv, fmt.Sprintf("DOCKER_CERT_PATH=%s", req.CAPath))
		}
	}

	if state == "absent" {
		// docker stack rm <name>
		cmd := exec.Command("docker", "stack", "rm", req.Name)
		cmd.Env = cmdEnv
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		if err != nil {
			// Check if it's because stack doesn't exist
			if strings.Contains(strings.ToLower(outputStr), "nothing found") ||
				strings.Contains(strings.ToLower(outputStr), "no such") {
				return Response{Changed: false, Msg: "stack already absent", Stdout: outputStr}
			}
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove stack: %v", err), Stdout: outputStr}
		}
		return Response{Changed: true, Msg: "stack removed", Stdout: outputStr}
	}

	// State present: docker stack deploy
	if req.ComposeFile == "" {
		return Response{Failed: true, Msg: "compose_file is required when state=present"}
	}

	// Check if compose file exists
	if _, err := os.Stat(req.ComposeFile); os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("compose_file does not exist: %s", req.ComposeFile)}
	}

	args := []string{"stack", "deploy", "-c", req.ComposeFile}

	if req.WithRegistryAuth {
		args = append(args, "--with-registry-auth")
	}
	if req.Prune {
		args = append(args, "--prune")
	}
	if req.ResolveImage != "" {
		args = append(args, "--resolve-image", req.ResolveImage)
	}

	args = append(args, req.Name)

	cmd := exec.Command("docker", args...)
	cmd.Env = cmdEnv
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to deploy stack: %v", err), Stdout: outputStr}
	}

	// Detect changes
	changed := false
	lowerOut := strings.ToLower(outputStr)
	keywords := []string{"creating", "updating", "removing"}
	for _, k := range keywords {
		if strings.Contains(lowerOut, k) {
			changed = true
			break
		}
	}

	return Response{Changed: changed, Msg: "stack deployed", Stdout: outputStr}
}
