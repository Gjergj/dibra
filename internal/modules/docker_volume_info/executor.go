package docker_volume_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

// Execute inspects a Docker volume and returns its full configuration
func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if req.Name == "" {
		return Response{Failed: true, Msg: "name is required"}
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	// Inspect volume
	result, err := cli.VolumeInspect(ctx, req.Name, client.VolumeInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "volume not found",
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect volume", req.Name, err).Error()}
	}
	inspect := result.Volume

	// Convert to map for flexible JSON output
	volume := map[string]interface{}{
		"name":       inspect.Name,
		"driver":     inspect.Driver,
		"mountpoint": inspect.Mountpoint,
		"created_at": inspect.CreatedAt,
		"scope":      inspect.Scope,
		"labels":     inspect.Labels,
		"options":    inspect.Options,
	}

	// Status (if available)
	if len(inspect.Status) > 0 {
		volume["status"] = inspect.Status
	}

	// Usage data (if available, requires API v1.42+)
	if inspect.UsageData != nil {
		volume["usage_data"] = map[string]interface{}{
			"size":      inspect.UsageData.Size,
			"ref_count": inspect.UsageData.RefCount,
		}
	}

	return Response{
		Changed: false, // Info modules never change anything
		Exists:  true,
		Volume:  volume,
	}
}
