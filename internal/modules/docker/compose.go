package docker

import (
	"fmt"
	"os"
	"strings"
)

// ComposeCommonArgs common arguments for Docker Compose modules
type ComposeCommonArgs struct {
	ProjectSrc  string            `json:"project_src"`
	ProjectName string            `json:"project_name"`
	Files       []string          `json:"files"`
	Env         map[string]string `json:"env"`
	Profiles    []string          `json:"profiles"`
}

// GetComposeBaseArgs returns the base CLI arguments for docker compose
func GetComposeBaseArgs(args ComposeCommonArgs) []string {
	var cliArgs []string
	cliArgs = append(cliArgs, "compose")

	if args.ProjectName != "" {
		cliArgs = append(cliArgs, "--project-name", args.ProjectName)
	}

	for _, f := range args.Files {
		cliArgs = append(cliArgs, "--file", f)
	}

	return cliArgs
}

// GetComposeEnv returns the environment variables for docker compose execution
func GetComposeEnv(args ComposeCommonArgs, common CommonArgs) []string {
	cmdEnv := os.Environ()

	// Apply connection args to Env
	if common.DockerHost != "" {
		cmdEnv = append(cmdEnv, fmt.Sprintf("DOCKER_HOST=%s", common.DockerHost))
	}
	if common.TLS || common.ValidateCerts {
		cmdEnv = append(cmdEnv, "DOCKER_TLS_VERIFY=1")
		// Note: More complex TLS setup might be needed if certs are not in default paths
	}

	// COMPOSE_PROFILES
	if len(args.Profiles) > 0 {
		cmdEnv = append(cmdEnv, fmt.Sprintf("COMPOSE_PROFILES=%s", strings.Join(args.Profiles, ",")))
	}

	// User defined env
	for k, v := range args.Env {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}

	return cmdEnv
}
