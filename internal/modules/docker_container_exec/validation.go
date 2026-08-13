package docker_container_exec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func validateRequest(req Request, environment docker.Environment) error {
	if !req.argumentProvided("container", req.Container != "") {
		return fmt.Errorf("container is required")
	}

	argvProvided := req.argumentProvided("argv", req.Argv != nil)
	commandProvided := req.argumentProvided("command", req.Command != "")
	if argvProvided == commandProvided {
		return fmt.Errorf("exactly one of argv or command must be specified")
	}
	if req.Detach && req.Stdin != nil {
		return fmt.Errorf("if detach=true, stdin cannot be provided")
	}
	for name, value := range req.Env {
		if _, ok := value.(string); !ok {
			return fmt.Errorf(
				"non-string value found for env option; ambiguous env values must be quoted or explicitly converted to strings; key: %s",
				name,
			)
		}
	}

	connection, err := docker.ResolveConnectionWithEnvironment(req.CommonArgs, environment)
	if err != nil {
		return err
	}
	if req.argumentProvided("chdir", req.Chdir != "") &&
		connection.APIVersion != "" && connection.APIVersion != "auto" &&
		apiVersionLessThan(connection.APIVersion, "1.35") {
		return fmt.Errorf("chdir requires Docker API version 1.35 or newer")
	}
	return nil
}

func apiVersionLessThan(actual, minimum string) bool {
	actualParts := strings.SplitN(strings.TrimPrefix(actual, "v"), ".", 3)
	minimumParts := strings.SplitN(strings.TrimPrefix(minimum, "v"), ".", 3)
	for index := 0; index < 2; index++ {
		actualValue := versionPart(actualParts, index)
		minimumValue := versionPart(minimumParts, index)
		if actualValue != minimumValue {
			return actualValue < minimumValue
		}
	}
	return false
}

func versionPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, _ := strconv.Atoi(parts[index])
	return value
}

func defaultTrue(value *bool) bool {
	return value == nil || *value
}
