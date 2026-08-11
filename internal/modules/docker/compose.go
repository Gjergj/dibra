package docker

import "strings"

// ComposeCommonArgs common arguments for Docker Compose modules
type ComposeCommonArgs struct {
	ProjectSrc  string            `json:"project_src"`
	ProjectName string            `json:"project_name"`
	Files       []string          `json:"files"`
	Env         map[string]string `json:"env"`
	Profiles    []string          `json:"profiles"`
}

// GetComposeBaseArgs returns the base CLI arguments for docker compose.
func GetComposeBaseArgs(args ComposeCommonArgs, common CommonArgs) ([]string, error) {
	return GetComposeBaseArgsWithEnvironment(args, common, OSEnvironment{})
}

// GetComposeBaseArgsWithEnvironment is the injectable form of
// GetComposeBaseArgs.
func GetComposeBaseArgsWithEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment) ([]string, error) {
	cliArgs, err := DockerCLIArgsWithEnvironment(common, environment, "compose")
	if err != nil {
		return nil, err
	}

	if args.ProjectName != "" {
		cliArgs = append(cliArgs, "--project-name", args.ProjectName)
	}

	for _, f := range args.Files {
		cliArgs = append(cliArgs, "--file", f)
	}

	return cliArgs, nil
}

// GetComposeEnv returns the environment variables for docker compose execution.
func GetComposeEnv(args ComposeCommonArgs, common CommonArgs) ([]string, error) {
	return GetComposeEnvWithEnvironment(args, common, OSEnvironment{})
}

// GetComposeEnvWithEnvironment is the injectable form of GetComposeEnv.
func GetComposeEnvWithEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment) ([]string, error) {
	cmdEnv, err := DockerCLIEnvWithEnvironment(common, environment)
	if err != nil {
		return nil, err
	}

	// COMPOSE_PROFILES
	if len(args.Profiles) > 0 {
		cmdEnv = setEnvironment(cmdEnv, "COMPOSE_PROFILES", strings.Join(args.Profiles, ","))
	}

	// User defined env
	for k, v := range args.Env {
		cmdEnv = setEnvironment(cmdEnv, k, v)
	}

	return cmdEnv, nil
}
