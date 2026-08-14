package docker_network_info

import (
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

// Execute inspects a Docker network and returns its full Engine configuration.
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

	result, err := cli.NetworkInspect(ctx, req.Name, client.NetworkInspectOptions{Verbose: true})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{Changed: false, Exists: false, Network: nil}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}

	network, err := inspectionFromResult(result)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}

	return Response{
		Changed: false,
		Exists:  true,
		Network: network,
	}
}

func inspectionFromResult(result client.NetworkInspectResult) (map[string]any, error) {
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Network)
}
