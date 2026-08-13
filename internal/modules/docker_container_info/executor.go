package docker_container_info

import (
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

// Execute inspects a Docker container and returns its full configuration
func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if !req.nameProvided() {
		return Response{Failed: true, Msg: "name is required"}
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	result, err := cli.ContainerInspect(ctx, req.Name, client.ContainerInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed:   false,
				Exists:    false,
				Container: nil,
			}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}

	encoded, err := json.Marshal(result.Container)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: encode container inspection: %v", err)}
	}
	var container map[string]interface{}
	if err := json.Unmarshal(encoded, &container); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: decode container inspection: %v", err)}
	}

	return Response{
		Changed:   false,
		Exists:    true,
		Container: container,
	}
}
