package docker_image_info

import (
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

// Execute inspects Docker images and returns their configuration
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

	// Try to inspect directly first (works for ID or exact name:tag)
	inspect, err := cli.ImageInspect(ctx, req.Name)
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{
				Changed: false,
				Exists:  false,
				Msg:     "image not found",
				Images:  []map[string]interface{}{},
			}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect image", req.Name, err).Error()}
	}

	// Convert to map for flexible JSON output
	img := convertImageToMap(inspect)

	return Response{
		Changed: false, // Info modules never change anything
		Exists:  true,
		Images:  []map[string]interface{}{img},
	}
}

func convertImageToMap(inspect client.ImageInspectResult) map[string]interface{} {
	img := map[string]interface{}{
		"id":           inspect.ID,
		"repo_tags":    inspect.RepoTags,
		"repo_digests": inspect.RepoDigests,
		"comment":      inspect.Comment,
		"created":      inspect.Created,
		"author":       inspect.Author,
		"architecture": inspect.Architecture,
		"os":           inspect.Os,
		"os_version":   inspect.OsVersion,
		"size":         inspect.Size,
		"virtual_size": inspect.Size, // VirtualSize is deprecated, use Size
	}

	// Config
	if inspect.Config != nil {
		config := map[string]interface{}{
			"user":          inspect.Config.User,
			"exposed_ports": inspect.Config.ExposedPorts,
			"env":           inspect.Config.Env,
			"cmd":           inspect.Config.Cmd,
			"volumes":       inspect.Config.Volumes,
			"working_dir":   inspect.Config.WorkingDir,
			"entrypoint":    inspect.Config.Entrypoint,
			"labels":        inspect.Config.Labels,
		}
		img["config"] = config
	}

	// RootFS
	if inspect.RootFS.Type != "" {
		img["root_fs"] = map[string]interface{}{
			"type":   inspect.RootFS.Type,
			"layers": inspect.RootFS.Layers,
		}
	}

	// GraphDriver
	if inspect.GraphDriver.Name != "" {
		img["graph_driver"] = map[string]interface{}{
			"name": inspect.GraphDriver.Name,
			"data": inspect.GraphDriver.Data,
		}
	}

	// Metadata
	if inspect.Metadata.LastTagTime.Unix() > 0 {
		img["metadata"] = map[string]interface{}{
			"last_tag_time": inspect.Metadata.LastTagTime,
		}
	}

	// Extract short ID (first 12 chars after sha256:)
	if strings.HasPrefix(inspect.ID, "sha256:") && len(inspect.ID) > 19 {
		img["short_id"] = inspect.ID[7:19]
	}

	return img
}
