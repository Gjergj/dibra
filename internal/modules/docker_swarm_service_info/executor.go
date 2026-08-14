package docker_swarm_service_info

import (
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

const cannotInspectServiceMsg = "Cannot inspect service: To inspect service execute module on Swarm Manager"

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if strings.TrimSpace(req.Name) == "" {
		return failed("missing required arguments: name")
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failed(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	if _, err := cli.SwarmInspect(ctx, client.SwarmInspectOptions{}); err != nil {
		return failed(docker.NotSwarmManagerMsg)
	}

	result, err := cli.ServiceInspect(ctx, req.Name, client.ServiceInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{Changed: false, Exists: false, Service: nil}
		}
		if errdefs.IsUnavailable(err) || strings.Contains(err.Error(), "503") {
			return failed(cannotInspectServiceMsg)
		}
		return failed(fmt.Sprintf("Error inspecting swarm service: %v", err))
	}

	service, err := inspectionFromResult(result)
	if err != nil {
		return failed(fmt.Sprintf("Error inspecting swarm service: %v", err))
	}
	return Response{
		Changed: false,
		Exists:  true,
		Service: service,
	}
}

func inspectionFromResult(result client.ServiceInspectResult) (map[string]any, error) {
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Service)
}

func failed(msg string) Response {
	return Response{Failed: true, Msg: msg}
}
