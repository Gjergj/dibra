package docker_volume_info

import (
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if !req.nameProvided() {
		return Response{Failed: true, Msg: "name is required"}
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	result, err := apiClient.VolumeInspect(ctx, req.Name, client.VolumeInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{Changed: false, Exists: false, Volume: nil}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("Error inspecting volume: %v", err)}
	}

	volume, err := inspectMap(result)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}
	return Response{Changed: false, Exists: true, Volume: volume}
}

func inspectMap(result client.VolumeInspectResult) (map[string]any, error) {
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Volume)
}
